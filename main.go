package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"dynamic-qris-go/qris"

	qrcode "github.com/skip2/go-qrcode"
)

const qrisImagePath = "data/qris.jpg"

type ConvertRequest struct {
	QRIS   string `json:"qris"`
	Amount int    `json:"amount"`
}

type ConvertResponse struct {
	Success     bool   `json:"success"`
	DynamicQRIS string `json:"dynamic_qris,omitempty"`
	QRImage     string `json:"qr_image,omitempty"` // base64-encoded PNG
	Error       string `json:"error,omitempty"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Determine web directory path
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	webDir := filepath.Join(execDir, "web")

	// Fallback: if running with `go run`, use current directory
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}

	// Serve static files
	fs := http.FileServer(http.Dir(webDir))
	http.Handle("/", fs)

	// API endpoints
	http.HandleFunc("/api/qris", handleGetQRIS)
	http.HandleFunc("/api/convert", handleConvert)

	addr := ":" + port
	log.Printf("🚀 Dynamic QRIS server running at http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ConvertResponse{
			Success: false,
			Error:   "Method not allowed. Use POST.",
		})
		return
	}

	var req ConvertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{
			Success: false,
			Error:   "Invalid JSON: " + err.Error(),
		})
		return
	}

	if req.QRIS == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{
			Success: false,
			Error:   "QRIS string is required",
		})
		return
	}

	if req.Amount <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{
			Success: false,
			Error:   "Amount must be greater than 0",
		})
		return
	}

	// Convert QRIS
	result, err := qris.Convert(req.QRIS, req.Amount)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ConvertResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Generate QR code image
	qrPNG, err := qrcode.Encode(result.DynamicQRIS, qrcode.Medium, 512)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ConvertResponse{
			Success: false,
			Error:   "Failed to generate QR code: " + err.Error(),
		})
		return
	}

	qrBase64 := base64.StdEncoding.EncodeToString(qrPNG)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ConvertResponse{
		Success:     true,
		DynamicQRIS: result.DynamicQRIS,
		QRImage:     "data:image/png;base64," + qrBase64,
	})

	log.Printf("✅ Converted QRIS | Amount: Rp %s", formatAmount(req.Amount))
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

// handleGetQRIS decodes the QRIS image from data/qris.jpg and returns the string.
// Decoded fresh each time so file changes are picked up automatically.
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

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"qris":    qrisString,
	})
}
