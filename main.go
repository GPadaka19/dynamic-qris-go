package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/textproto"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dynamic-qris-go/qris"

	qrcode "github.com/skip2/go-qrcode"
)

// ─── Config ──────────────────────────────────────────────────────────────────

const qrisImagePath = "data/qris.jpg"

type config struct {
	Port       string
	WAApiURL   string
	WAUser     string
	WAPassword string
}

var cfg config

func loadConfig() {
	// Load .env file first (values already set in env take precedence)
	loadEnvFile(".env")

	cfg = config{
		Port:       getEnv("PORT", "8080"),
		WAApiURL:   strings.TrimRight(getEnv("WA_API_URL", ""), "/"),
		WAUser:     getEnv("WA_USER", ""),
		WAPassword: getEnv("WA_PASSWORD", ""),
	}
}

// loadEnvFile parses a .env file and sets variables that are not already in the environment.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ─── Types ───────────────────────────────────────────────────────────────────

type ConvertRequest struct {
	QRIS    string `json:"qris"`
	Amount  int    `json:"amount"`
	Remarks string `json:"remarks"`
}

type ConvertResponse struct {
	Success      bool   `json:"success"`
	DynamicQRIS  string `json:"dynamic_qris,omitempty"`
	QRImage      string `json:"qr_image,omitempty"` // base64 data URL
	MerchantName string `json:"merchant_name,omitempty"`
	Remarks      string `json:"remarks,omitempty"`
	Error        string `json:"error,omitempty"`
}

type SendRequest struct {
	Phone   string `json:"phone"`
	Caption string `json:"caption"`
	Image   string `json:"image"` // data URL: data:image/png;base64,...
}

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
	loadConfig()

	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	webDir := filepath.Join(execDir, "web")
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}

	http.Handle("/", http.FileServer(http.Dir(webDir)))
	http.HandleFunc("/api/qris", handleGetQRIS)
	http.HandleFunc("/api/convert", handleConvert)
	http.HandleFunc("/api/contacts", handleGetContacts)
	http.HandleFunc("/api/send", handleSend)

	addr := ":" + cfg.Port
	log.Printf("🚀 Dynamic QRIS server running at http://localhost%s", addr)
	if cfg.WAApiURL != "" {
		log.Printf("📱 WA API: %s (user: %s)", cfg.WAApiURL, cfg.WAUser)
	} else {
		log.Printf("⚠️  WA_API_URL not set — /api/contacts and /api/send will return errors")
	}
	log.Fatal(http.ListenAndServe(addr, nil))
}

// ─── QRIS Handlers ───────────────────────────────────────────────────────────

func handleGetQRIS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Use GET"})
		return
	}

	qrisString, err := qris.DecodeQRImage(qrisImagePath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "Gagal membaca QRIS: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"success": true, "qris": qrisString})
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ConvertResponse{Success: false, Error: "Use POST"})
		return
	}

	var req ConvertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{Success: false, Error: "Invalid JSON: " + err.Error()})
		return
	}

	if req.QRIS == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{Success: false, Error: "QRIS string is required"})
		return
	}
	if req.Amount <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{Success: false, Error: "Amount must be greater than 0"})
		return
	}

	result, err := qris.Convert(req.QRIS, req.Amount, req.Remarks)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{Success: false, Error: err.Error()})
		return
	}

	qrPNG, err := qrcode.Encode(result.DynamicQRIS, qrcode.Medium, 512)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ConvertResponse{Success: false, Error: "Failed to generate QR: " + err.Error()})
		return
	}

	merchantName := extractTag(req.QRIS, "59")

	json.NewEncoder(w).Encode(ConvertResponse{
		Success:      true,
		DynamicQRIS:  result.DynamicQRIS,
		QRImage:      "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG),
		MerchantName: merchantName,
		Remarks:      req.Remarks,
	})

	log.Printf("✅ Converted QRIS | Amount: Rp %s | Merchant: %s", formatAmount(req.Amount), merchantName)
}

// ─── WA Proxy Handlers ───────────────────────────────────────────────────────

// handleGetContacts proxies GET /user/my/contacts from the WA gateway.
func handleGetContacts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Use GET"})
		return
	}

	if cfg.WAApiURL == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "WA_API_URL belum dikonfigurasi di server",
		})
		return
	}

	waReq, err := http.NewRequest(http.MethodGet, cfg.WAApiURL+"/user/my/contacts", nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
		return
	}
	if cfg.WAUser != "" {
		waReq.SetBasicAuth(cfg.WAUser, cfg.WAPassword)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(waReq)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Gagal menghubungi WA API: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleSend proxies POST /send/image to the WA gateway.
// Accepts JSON {phone, caption, image (data URL)} and forwards as multipart/form-data.
func handleSend(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Use POST"})
		return
	}

	if cfg.WAApiURL == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "WA_API_URL belum dikonfigurasi di server",
		})
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Invalid JSON: " + err.Error()})
		return
	}

	if req.Phone == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "phone is required"})
		return
	}
	if req.Image == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "image is required"})
		return
	}

	// Decode base64 data URL → raw bytes
	imgBytes, err := decodeDataURL(req.Image)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Invalid image data: " + err.Error()})
		return
	}

	// Build multipart/form-data body
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("phone", req.Phone)
	mw.WriteField("caption", req.Caption)
	mw.WriteField("view_once", "false")
	mw.WriteField("compress", "false")
	mw.WriteField("is_forwarded", "false")

	// CreateFormFile uses application/octet-stream by default — set image/png explicitly
	// so the WA gateway accepts it.
	imgHeader := make(textproto.MIMEHeader)
	imgHeader.Set("Content-Disposition", `form-data; name="image"; filename="qris.png"`)
	imgHeader.Set("Content-Type", "image/png")
	fw, err := mw.CreatePart(imgHeader)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
		return
	}
	fw.Write(imgBytes)
	mw.Close()

	waReq, err := http.NewRequest(http.MethodPost, cfg.WAApiURL+"/send/image", &buf)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
		return
	}
	waReq.Header.Set("Content-Type", mw.FormDataContentType())
	if cfg.WAUser != "" {
		waReq.SetBasicAuth(cfg.WAUser, cfg.WAPassword)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(waReq)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Gagal mengirim ke WA API: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	log.Printf("📤 Sent QRIS to %s | WA API status: %d", req.Phone, resp.StatusCode)

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func decodeDataURL(dataURL string) ([]byte, error) {
	// Format: data:<mime>;base64,<data>
	idx := strings.Index(dataURL, ",")
	if idx == -1 {
		return nil, io.ErrUnexpectedEOF
	}
	return base64.StdEncoding.DecodeString(dataURL[idx+1:])
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
	var result []byte
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			result = append(result, '.')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
