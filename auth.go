package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "qris_session"
	sessionTTL    = 24 * time.Hour

	maxLoginBody  = 4 << 10
	maxLoginFails = 5
	loginWindow   = time.Minute
)

// publicPaths are reachable without a session: the login page and its assets, and the health check.
var publicPaths = map[string]bool{
	"/login":      true,
	"/style.css":  true,
	"/healthz":    true,
	"/api/login":  true,
	"/api/logout": true,
}

type attempt struct {
	fails int
	reset time.Time
}

// authenticator guards the app with a single shared password (APP_PASSWORD).
// Sessions are stateless HMAC-signed cookies. It is disabled when no password is set.
type authenticator struct {
	enabled  bool
	passHash [sha256.Size]byte
	key      []byte
	now      func() time.Time

	mu       sync.Mutex
	attempts map[string]*attempt
}

func newAuthenticator(cfg config) *authenticator {
	a := &authenticator{
		enabled:  cfg.AppPassword != "",
		passHash: sha256.Sum256([]byte(cfg.AppPassword)),
		now:      time.Now,
		attempts: make(map[string]*attempt),
	}
	if !a.enabled {
		return a
	}

	a.key = []byte(cfg.SessionSecret)
	if len(a.key) == 0 {
		// No fixed secret: sessions are valid only until the process restarts.
		a.key = make([]byte, 32)
		if _, err := rand.Read(a.key); err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
	}
	return a
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (a *authenticator) sign(expiry string) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte("v1|" + expiry + "|"))
	mac.Write(a.passHash[:]) // changing the password invalidates existing sessions
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *authenticator) newToken() string {
	expiry := strconv.FormatInt(a.now().Add(sessionTTL).Unix(), 10)
	return expiry + "." + a.sign(expiry)
}

func (a *authenticator) validToken(token string) bool {
	expiry, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(a.sign(expiry))) {
		return false
	}
	exp, err := strconv.ParseInt(expiry, 10, 64)
	return err == nil && a.now().Unix() < exp
}

func (a *authenticator) authenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	return err == nil && a.validToken(c.Value)
}

func (a *authenticator) checkPassword(password string) bool {
	h := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(h[:], a.passHash[:]) == 1
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
}

// ─── Brute-force throttle ────────────────────────────────────────────────────

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// blocked reports whether key has used up its failed attempts in the current window.
func (a *authenticator) blocked(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	at, ok := a.attempts[key]
	if !ok {
		return false
	}
	if !a.now().Before(at.reset) {
		delete(a.attempts, key)
		return false
	}
	return at.fails >= maxLoginFails
}

func (a *authenticator) recordFail(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if len(a.attempts) > 1000 { // bound memory under a flood of distinct addresses
		for k, v := range a.attempts {
			if !now.Before(v.reset) {
				delete(a.attempts, k)
			}
		}
	}

	at, ok := a.attempts[key]
	if !ok || !now.Before(at.reset) {
		at = &attempt{reset: now.Add(loginWindow)}
		a.attempts[key] = at
	}
	at.fails++
}

func (a *authenticator) clearFails(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.attempts, key)
}

// ─── Middleware ──────────────────────────────────────────────────────────────

func (a *authenticator) middleware(next http.Handler) http.Handler {
	if !a.enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] || a.authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if !s.auth.enabled || s.auth.authenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(s.cfg.WebDir, "login.html"))
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.enabled {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}

	key := clientKey(r)
	if s.auth.blocked(key) {
		w.Header().Set("Retry-After", strconv.Itoa(int(loginWindow.Seconds())))
		writeError(w, http.StatusTooManyRequests, "Terlalu banyak percobaan. Coba lagi sebentar.")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if !decodeBody(w, r, maxLoginBody, &req) {
		return
	}

	if !s.auth.checkPassword(req.Password) {
		s.auth.recordFail(key)
		s.log.Warn("failed login", "ip", key)
		writeError(w, http.StatusUnauthorized, "Password salah")
		return
	}

	s.auth.clearFails(key)
	setSessionCookie(w, r, s.auth.newToken(), int(sessionTTL.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	setSessionCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// handleSession tells the frontend whether a login is required (and so whether to show logout).
func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "auth": s.auth.enabled})
}

// securityHeaders adds baseline hardening headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
