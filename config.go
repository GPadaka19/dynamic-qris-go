package main

import (
	"bufio"
	"os"
	"strings"
)

type config struct {
	Port       string
	WebDir     string
	QRISPath   string
	WAApiURL   string
	WAUser     string
	WAPassword string
	WADeviceID string

	AppPassword   string // empty disables the login page
	SessionSecret string // optional; random per process when empty
}

func loadConfig() config {
	// .env is optional; values already set in the environment take precedence.
	loadEnvFile(".env")

	return config{
		Port:       getEnv("PORT", "4009"),
		WebDir:     getEnv("WEB_DIR", "web"),
		QRISPath:   getEnv("QRIS_IMAGE_PATH", "data/qris.jpg"),
		WAApiURL:   strings.TrimRight(getEnv("WA_API_URL", ""), "/"),
		WAUser:     getEnv("WA_USER", ""),
		WAPassword: getEnv("WA_PASSWORD", ""),
		WADeviceID: getEnv("WA_DEVICE_ID", ""),

		AppPassword:   getEnv("APP_PASSWORD", ""),
		SessionSecret: getEnv("SESSION_SECRET", ""),
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
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			// Setenv only fails for an invalid key; skipping such a line is fine.
			_ = os.Setenv(key, val)
		}
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
