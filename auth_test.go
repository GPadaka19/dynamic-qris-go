package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authServer(t *testing.T) (*server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "INDEX", "login.html": "LOGIN", "style.css": "CSS"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := testServer(t, config{WebDir: dir, AppPassword: "s3cret", SessionSecret: "k"})
	return s, s.routes()
}

func login(t *testing.T, h http.Handler, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body)))
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func get(h http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthDisabledWhenNoPassword(t *testing.T) {
	h := testServer(t, config{}).routes()
	if rec := do(t, h, http.MethodGet, "/api/session", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"auth":false`) {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestAuthRequired(t *testing.T) {
	_, h := authServer(t)

	if rec := get(h, "/api/qris", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("api without session = %d, want 401", rec.Code)
	}
	if rec := get(h, "/", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("page without session = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "/login", nil); rec.Code != http.StatusOK || rec.Body.String() != "LOGIN" {
		t.Errorf("login page = %d %q", rec.Code, rec.Body)
	}
	if rec := get(h, "/style.css", nil); rec.Code != http.StatusOK {
		t.Errorf("style.css = %d", rec.Code)
	}
	if rec := get(h, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("healthz = %d", rec.Code)
	}
	if rec := get(h, "/login.html", nil); rec.Code == http.StatusOK {
		t.Errorf("login.html served directly without redirect handling")
	}
}

func TestLoginFlow(t *testing.T) {
	_, h := authServer(t)

	if rec := login(t, h, "wrong"); rec.Code != http.StatusUnauthorized || sessionOf(rec) != nil {
		t.Fatalf("wrong password: status = %d, cookie = %v", rec.Code, sessionOf(rec))
	}

	rec := login(t, h, "s3cret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	c := sessionOf(rec)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("bad session cookie: %+v", c)
	}

	if rec := get(h, "/", c); rec.Code != http.StatusOK || rec.Body.String() != "INDEX" {
		t.Errorf("index with session = %d %q", rec.Code, rec.Body)
	}
	if rec := get(h, "/login", c); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("login page with session = %d", rec.Code)
	}
	if rec := get(h, "/api/session", c); !strings.Contains(rec.Body.String(), `"auth":true`) {
		t.Errorf("session body = %s", rec.Body)
	}
}

func TestSessionTamperedAndExpired(t *testing.T) {
	s, h := authServer(t)
	c := sessionOf(login(t, h, "s3cret"))

	tampered := *c
	tampered.Value = "9999999999." + strings.SplitN(c.Value, ".", 2)[1]
	if rec := get(h, "/api/qris", &tampered); rec.Code != http.StatusUnauthorized {
		t.Errorf("tampered expiry = %d, want 401", rec.Code)
	}

	s.auth.now = func() time.Time { return time.Now().Add(sessionTTL + time.Minute) }
	if rec := get(h, "/api/session", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired session = %d, want 401", rec.Code)
	}
}

func TestSessionInvalidatedByPasswordChange(t *testing.T) {
	_, h := authServer(t)
	c := sessionOf(login(t, h, "s3cret"))

	other := testServer(t, config{WebDir: t.TempDir(), AppPassword: "changed", SessionSecret: "k"}).routes()
	if rec := get(other, "/api/session", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("session accepted after password change = %d", rec.Code)
	}
}

func TestLoginThrottle(t *testing.T) {
	s, h := authServer(t)
	for i := 0; i < maxLoginFails; i++ {
		if rec := login(t, h, "nope"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, rec.Code)
		}
	}
	if rec := login(t, h, "s3cret"); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("correct password while blocked = %d", rec.Code)
	}

	s.auth.now = func() time.Time { return time.Now().Add(loginWindow + time.Second) }
	if rec := login(t, h, "s3cret"); rec.Code != http.StatusOK {
		t.Errorf("after window = %d", rec.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	_, h := authServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if c := sessionOf(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("logout cookie = %+v", c)
	}
}
