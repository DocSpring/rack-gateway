package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

func writeJSON(w http.ResponseWriter, payload interface{}) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		mclog.Errorf("failed to encode JSON response: %v", err)
	}
}

func decodeRequest(body io.ReadCloser, dest interface{}) error {
	defer func() {
		_ = body.Close()
	}()
	return json.NewDecoder(body).Decode(dest)
}

func nextID(base string) string {
	return fmt.Sprintf("%s-%04d", base, idCounter.Add(1))
}

func isObjectUploadPath(p string) bool {
	return strings.Contains(p, "/objects/tmp/")
}

func truncateForLog(body string) string {
	const maxPreviewBytes = 4096
	if len(body) <= maxPreviewBytes {
		return body
	}
	return body[:maxPreviewBytes] + "…(truncated)"
}

func defaultEnvMap() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"REDIS_URL":    "redis://localhost:6379",
		"SECRET_KEY":   "super-secret-key-12345",
		"NODE_ENV":     "production",
		"PORT":         "3000",
	}
}

func envString() string {
	env := defaultEnvMap()
	order := []string{"DATABASE_URL", "REDIS_URL", "SECRET_KEY", "NODE_ENV", "PORT"}
	var b strings.Builder
	for _, k := range order {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return b.String()
}

func parseSubprotocols(h string) []string {
	if h == "" {
		return nil
	}
	parts := strings.Split(h, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
