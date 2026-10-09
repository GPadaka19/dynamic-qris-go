package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"dynamic-qris-go/qris"
)

func testServer(t *testing.T, cfg config) *server {
	t.Helper()
	if cfg.WebDir == "" {
		cfg.WebDir = t.TempDir()
	}
	return newServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func validQRIS() string {
	data := "00020101021153033605802ID5904TEST6007JAKARTA6304"
	return data + qris.CRC16CCITT(data)
}

func TestHealth(t *testing.T) {
	rec := do(t, testServer(t, config{}).routes(), http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := testServer(t, config{}).routes()
	if rec := do(t, h, http.MethodGet, "/api/convert", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/convert status = %d, want 405", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/api/qris", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/qris status = %d, want 405", rec.Code)
	}
}

func TestConvert(t *testing.T) {
	h := testServer(t, config{}).routes()

	t.Run("valid", func(t *testing.T) {
		body, _ := json.Marshal(ConvertRequest{QRIS: validQRIS(), Amount: 50000})
		rec := do(t, h, http.MethodPost, "/api/convert", string(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		var resp ConvertResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Success || resp.DynamicQRIS == "" || !strings.HasPrefix(resp.QRImage, pngDataURL) || resp.MerchantName != "TEST" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	cases := map[string]struct {
		body string
		want int
	}{
		"invalid json":   {`{`, http.StatusBadRequest},
		"missing qris":   {`{"amount":1000}`, http.StatusBadRequest},
		"zero amount":    {`{"qris":"x","amount":0}`, http.StatusBadRequest},
		"invalid qris":   {`{"qris":"garbage","amount":1000}`, http.StatusBadRequest},
		"body too large": {`{"qris":"` + strings.Repeat("a", maxConvertBody) + `","amount":1}`, http.StatusRequestEntityTooLarge},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(t, h, http.MethodPost, "/api/convert", c.body)
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestWANotConfigured(t *testing.T) {
	h := testServer(t, config{}).routes()
	if rec := do(t, h, http.MethodGet, "/api/contacts", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("contacts status = %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/api/send", `{}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("send status = %d", rec.Code)
	}
}

func TestContactsProxyAndCache(t *testing.T) {
	var hits atomic.Int32
	wa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/user/my/contacts" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "user" || p != "pass" {
			t.Errorf("basic auth = %q %q %v", u, p, ok)
		}
		if got := r.Header.Get("X-Device-Id"); got != "dev1" {
			t.Errorf("X-Device-Id = %q", got)
		}
		_, _ = io.WriteString(w, `{"code":"SUCCESS","results":{"data":[]}}`)
	}))
	defer wa.Close()

	h := testServer(t, config{WAApiURL: wa.URL, WAUser: "user", WAPassword: "pass", WADeviceID: "dev1"}).routes()
	for i := 0; i < 3; i++ {
		rec := do(t, h, http.MethodGet, "/api/contacts", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SUCCESS") {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("WA gateway hit %d times, want 1 (cached)", n)
	}
}

func TestContactsErrorNotCached(t *testing.T) {
	var hits atomic.Int32
	wa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"code":"ERR","message":"boom"}`)
	}))
	defer wa.Close()

	h := testServer(t, config{WAApiURL: wa.URL}).routes()
	for i := 0; i < 2; i++ {
		if rec := do(t, h, http.MethodGet, "/api/contacts", ""); rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("WA gateway hit %d times, want 2", n)
	}
}

func TestSend(t *testing.T) {
	png := []byte("\x89PNG fake")
	wa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send/image" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("phone"); got != "628123" {
			t.Errorf("phone = %q", got)
		}
		if got := r.FormValue("compress"); got != "false" {
			t.Errorf("compress = %q", got)
		}
		f, hdr, err := r.FormFile("image")
		if err != nil {
			t.Fatalf("image: %v", err)
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != string(png) || hdr.Header.Get("Content-Type") != "image/png" {
			t.Errorf("image = %q (%s)", data, hdr.Header.Get("Content-Type"))
		}
		_, _ = io.WriteString(w, `{"code":"SUCCESS"}`)
	}))
	defer wa.Close()

	h := testServer(t, config{WAApiURL: wa.URL}).routes()
	img := pngDataURL + base64.StdEncoding.EncodeToString(png)

	body, _ := json.Marshal(SendRequest{Phone: "628123", Caption: "hi", Image: img})
	if rec := do(t, h, http.MethodPost, "/api/send", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	bad := map[string]SendRequest{
		"no phone":     {Image: img},
		"bad phone":    {Phone: "62 8;", Image: img},
		"no image":     {Phone: "628123"},
		"not data url": {Phone: "628123", Image: "http://evil/x.png"},
		"wrong mime":   {Phone: "628123", Image: "data:text/html;base64,AAAA"},
		"invalid b64":  {Phone: "628123", Image: pngDataURL + "!!!"},
	}
	for name, req := range bad {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(req)
			if rec := do(t, h, http.MethodPost, "/api/send", string(b)); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestSendGatewayDown(t *testing.T) {
	wa := httptest.NewServer(http.NotFoundHandler())
	url := wa.URL
	wa.Close()

	h := testServer(t, config{WAApiURL: url}).routes()
	body, _ := json.Marshal(SendRequest{Phone: "628123", Image: pngDataURL + "AAAA"})
	rec := do(t, h, http.MethodPost, "/api/send", string(body))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), url) {
		t.Errorf("error leaks gateway URL: %s", rec.Body)
	}
}

func TestFormatAmount(t *testing.T) {
	for in, want := range map[int]string{5: "5", 999: "999", 1000: "1.000", 50000: "50.000", 1234567: "1.234.567"} {
		if got := formatAmount(in); got != want {
			t.Errorf("formatAmount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.env"
	content := "# c\nexport T_A=\"one\"\nT_B='two'\nT_C=three=3\nT_KEEP=file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T_KEEP", "env")
	for _, k := range []string{"T_A", "T_B", "T_C"} {
		t.Setenv(k, "")
	}

	loadEnvFile(path)
	for k, want := range map[string]string{"T_A": "one", "T_B": "two", "T_C": "three=3", "T_KEEP": "env"} {
		if got := getEnv(k, ""); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
