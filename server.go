package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dynamic-qris-go/qris"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	maxConvertBody  = 64 << 10 // JSON with a QRIS string
	maxSendBody     = 5 << 20  // JSON with a base64 PNG
	maxContactsBody = 10 << 20 // contacts list from the WA gateway
	maxSendRespBody = 1 << 20  // send response from the WA gateway

	contactsTTL = 5 * time.Minute
	pngDataURL  = "data:image/png;base64,"
)

var phoneRe = regexp.MustCompile(`^[0-9A-Za-z@.\-]{3,64}$`)

// ─── Types ───────────────────────────────────────────────────────────────────

type ConvertRequest struct {
	QRIS    string `json:"qris"`
	Amount  int    `json:"amount"`
	Remarks string `json:"remarks"`
}

type ConvertResponse struct {
	Success      bool   `json:"success"`
	DynamicQRIS  string `json:"dynamic_qris"`
	QRImage      string `json:"qr_image"` // base64 data URL
	MerchantName string `json:"merchant_name"`
	Remarks      string `json:"remarks,omitempty"`
}

type SendRequest struct {
	Phone   string `json:"phone"`
	Caption string `json:"caption"`
	Image   string `json:"image"` // data URL: data:image/png;base64,...
}

// ─── Server ──────────────────────────────────────────────────────────────────

type server struct {
	cfg    config
	log    *slog.Logger
	client *http.Client
	auth   *authenticator
	now    func() time.Time

	mu       sync.Mutex
	qrisMod  time.Time // modtime of the image the cached text was decoded from
	qrisText string
	contacts []byte
	contExp  time.Time
}

func newServer(cfg config, logger *slog.Logger) *server {
	return &server{
		cfg:    cfg,
		log:    logger,
		client: &http.Client{Timeout: 30 * time.Second},
		auth:   newAuthenticator(cfg),
		now:    time.Now,
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(s.cfg.WebDir)))
	mux.HandleFunc("/login", only(http.MethodGet, s.handleLoginPage))
	mux.HandleFunc("/api/login", only(http.MethodPost, s.handleLogin))
	mux.HandleFunc("/api/logout", only(http.MethodPost, s.handleLogout))
	mux.HandleFunc("/api/session", only(http.MethodGet, s.handleSession))
	mux.HandleFunc("/healthz", only(http.MethodGet, s.handleHealth))
	mux.HandleFunc("/api/qris", only(http.MethodGet, s.handleGetQRIS))
	mux.HandleFunc("/api/convert", only(http.MethodPost, s.handleConvert))
	mux.HandleFunc("/api/contacts", only(http.MethodGet, s.handleGetContacts))
	mux.HandleFunc("/api/send", only(http.MethodPost, s.handleSend))
	return securityHeaders(s.auth.middleware(mux))
}

// only restricts h to a single HTTP method, answering others with a JSON 405.
func only(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed, "Use "+method)
			return
		}
		h(w, r)
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *server) handleGetQRIS(w http.ResponseWriter, r *http.Request) {
	text, err := s.loadQRIS()
	if err != nil {
		s.log.Error("read QRIS image", "path", s.cfg.QRISPath, "error", err)
		writeError(w, http.StatusInternalServerError, "Gagal membaca QRIS dari data/qris.jpg")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "qris": text})
}

// loadQRIS decodes the QRIS image, re-decoding only when the file has changed.
func (s *server) loadQRIS() (string, error) {
	info, err := os.Stat(s.cfg.QRISPath)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.qrisText != "" && info.ModTime().Equal(s.qrisMod) {
		return s.qrisText, nil
	}

	text, err := qris.DecodeQRImage(s.cfg.QRISPath)
	if err != nil {
		return "", err
	}
	s.qrisText, s.qrisMod = text, info.ModTime()
	return text, nil
}

func (s *server) handleConvert(w http.ResponseWriter, r *http.Request) {
	var req ConvertRequest
	if !decodeBody(w, r, maxConvertBody, &req) {
		return
	}

	if req.QRIS == "" {
		writeError(w, http.StatusBadRequest, "QRIS string is required")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "Amount must be greater than 0")
		return
	}

	result, err := qris.Convert(req.QRIS, req.Amount, req.Remarks)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	qrPNG, err := qrcode.Encode(result.DynamicQRIS, qrcode.Medium, 512)
	if err != nil {
		s.log.Error("encode QR image", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to generate QR")
		return
	}

	merchantName := extractTag(req.QRIS, "59")
	writeJSON(w, http.StatusOK, ConvertResponse{
		Success:      true,
		DynamicQRIS:  result.DynamicQRIS,
		QRImage:      pngDataURL + base64.StdEncoding.EncodeToString(qrPNG),
		MerchantName: merchantName,
		Remarks:      req.Remarks,
	})

	s.log.Info("converted QRIS", "amount", "Rp "+formatAmount(req.Amount), "merchant", merchantName)
}

// handleGetContacts proxies GET /user/my/contacts from the WA gateway,
// caching successful responses for contactsTTL.
func (s *server) handleGetContacts(w http.ResponseWriter, r *http.Request) {
	if !s.waConfigured(w) {
		return
	}

	s.mu.Lock()
	cached, fresh := s.contacts, s.now().Before(s.contExp)
	s.mu.Unlock()
	if fresh && cached != nil {
		writeRaw(w, http.StatusOK, cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := s.waRequest(ctx, http.MethodGet, "/user/my/contacts", nil, "")
	if err != nil {
		s.log.Error("build WA contacts request", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	status, body, err := s.doWA(req, maxContactsBody)
	if err != nil {
		s.log.Error("WA contacts request", "error", err)
		writeError(w, http.StatusBadGateway, "Gagal menghubungi WA API")
		return
	}

	if status == http.StatusOK {
		s.mu.Lock()
		s.contacts, s.contExp = body, s.now().Add(contactsTTL)
		s.mu.Unlock()
	}
	writeRaw(w, status, body)
}

// handleSend proxies POST /send/image to the WA gateway.
// Accepts JSON {phone, caption, image (data URL)} and forwards as multipart/form-data.
func (s *server) handleSend(w http.ResponseWriter, r *http.Request) {
	if !s.waConfigured(w) {
		return
	}

	var req SendRequest
	if !decodeBody(w, r, maxSendBody, &req) {
		return
	}

	if !phoneRe.MatchString(req.Phone) {
		writeError(w, http.StatusBadRequest, "phone is required and must be a valid number")
		return
	}
	if req.Image == "" {
		writeError(w, http.StatusBadRequest, "image is required")
		return
	}

	imgBytes, err := decodePNGDataURL(req.Image)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid image data: "+err.Error())
		return
	}

	body, contentType, err := buildSendForm(req, imgBytes)
	if err != nil {
		s.log.Error("build WA send form", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	waReq, err := s.waRequest(r.Context(), http.MethodPost, "/send/image", body, contentType)
	if err != nil {
		s.log.Error("build WA send request", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	status, respBody, err := s.doWA(waReq, maxSendRespBody)
	if err != nil {
		s.log.Error("WA send request", "error", err)
		writeError(w, http.StatusBadGateway, "Gagal mengirim ke WA API")
		return
	}

	s.log.Info("sent QRIS", "phone", req.Phone, "wa_status", status)
	writeRaw(w, status, respBody)
}

// ─── WA gateway helpers ──────────────────────────────────────────────────────

func (s *server) waConfigured(w http.ResponseWriter) bool {
	if s.cfg.WAApiURL == "" {
		writeError(w, http.StatusServiceUnavailable, "WA_API_URL belum dikonfigurasi di server")
		return false
	}
	return true
}

func (s *server) waRequest(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.WAApiURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if s.cfg.WAUser != "" {
		req.SetBasicAuth(s.cfg.WAUser, s.cfg.WAPassword)
	}
	if s.cfg.WADeviceID != "" {
		req.Header.Set("X-Device-Id", s.cfg.WADeviceID)
	}
	return req, nil
}

// doWA performs req and returns the status and body, failing if the body exceeds limit bytes.
func (s *server) doWA(req *http.Request, limit int64) (int, []byte, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, nil, err
	}
	if int64(len(body)) > limit {
		return 0, nil, fmt.Errorf("WA response exceeds %d bytes", limit)
	}
	return resp.StatusCode, body, nil
}

func buildSendForm(req SendRequest, img []byte) (io.Reader, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fields := [][2]string{
		{"phone", req.Phone},
		{"caption", req.Caption},
		{"view_once", "false"},
		{"compress", "false"}, // compression would degrade the QR code
		{"is_forwarded", "false"},
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return nil, "", err
		}
	}

	// CreateFormFile uses application/octet-stream by default — set image/png explicitly
	// so the WA gateway accepts it.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="image"; filename="qris.png"`)
	h.Set("Content-Type", "image/png")
	fw, err := mw.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(img); err != nil {
		return nil, "", err
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return &buf, mw.FormDataContentType(), nil
}

// ─── HTTP helpers ────────────────────────────────────────────────────────────

// decodeBody reads a size-limited JSON body into v, writing the error response itself on failure.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "Request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeRaw(w, http.StatusInternalServerError, []byte(`{"success":false,"error":"Internal error","message":"Internal error"}`))
		return
	}
	writeRaw(w, status, body)
}

// writeError sends {success, error, message}. The WA gateway reports errors as
// `message`, so both keys are set to keep the frontend handling uniform.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"success": false, "error": msg, "message": msg})
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

var errBadDataURL = errors.New("expected a base64 PNG data URL")

func decodePNGDataURL(dataURL string) ([]byte, error) {
	if !strings.HasPrefix(dataURL, pngDataURL) {
		return nil, errBadDataURL
	}
	return base64.StdEncoding.DecodeString(dataURL[len(pngDataURL):])
}

func extractTag(qrisString, tag string) string {
	tlvs, err := qris.Parse(strings.TrimSpace(qrisString))
	if err != nil {
		return ""
	}
	for _, tlv := range tlvs {
		if tlv.Tag == tag {
			return tlv.Value
		}
	}
	return ""
}

func formatAmount(amount int) string {
	s := strconv.Itoa(amount)
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return b.String()
}
