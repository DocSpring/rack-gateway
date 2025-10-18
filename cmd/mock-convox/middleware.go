package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(p []byte) (int, error) {
	if len(p) > 0 {
		sr.body = append(sr.body, p...)
	}
	return sr.ResponseWriter.Write(p)
}

func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := sr.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijacker not supported")
}

func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if mclog.TopicEnabled(mclog.TopicHTTP) {
			mclog.DebugTopicf(mclog.TopicHTTP, "request %s %s rawQuery=%q", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if mclog.TopicEnabled(mclog.TopicHTTPHeaders) {
			for k, vs := range r.Header {
				for _, v := range vs {
					mclog.DebugTopicf(mclog.TopicHTTPHeaders, "%s: %s", k, v)
				}
			}
		}
		if mclog.TopicEnabled(mclog.TopicHTTPRequest) && r.Body != nil && !isObjectUploadPath(r.URL.Path) && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			buf, err := io.ReadAll(r.Body)
			if err != nil {
				mclog.Warnf("failed to read request body: %v", err)
			} else {
				if err := r.Body.Close(); err != nil {
					mclog.Warnf("failed to close request body: %v", err)
				}
				r.Body = io.NopCloser(strings.NewReader(string(buf)))
				preview := truncateForLog(string(buf))
				mclog.DebugTopicf(mclog.TopicHTTPRequest, "body (%d bytes): %s", len(buf), preview)
			}
		}
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		if mclog.TopicEnabled(mclog.TopicHTTPResponse) && !isObjectUploadPath(r.URL.Path) {
			preview := truncateForLog(string(sr.body))
			mclog.DebugTopicf(mclog.TopicHTTPResponse, "body (%d bytes): %s", len(sr.body), preview)
		}
		if mclog.TopicEnabled(mclog.TopicHTTP) {
			mclog.DebugTopicf(mclog.TopicHTTP, "response %d %s %s in %s", sr.status, r.Method, r.URL.String(), time.Since(start))
		}
	})
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mock Convox"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		const prefix = "Basic "
		if !strings.HasPrefix(auth, prefix) {
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
		if err != nil {
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 || parts[0] != mockUsername || parts[1] != mockPassword {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
