package middleware

import (
	"net/http"
	"testing"
)

func TestClientIPFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		setupReq   func() *http.Request
		expectedIP string
	}{
		{
			name: "nil request returns empty string",
			setupReq: func() *http.Request {
				return nil
			},
			expectedIP: "",
		},
		{
			name: "extracts from X-Forwarded-For header",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Forwarded-For", "192.0.2.1, 203.0.113.1")
				req.RemoteAddr = "198.51.100.1:12345"
				return req
			},
			expectedIP: "192.0.2.1",
		},
		{
			name: "extracts from X-Real-IP when X-Forwarded-For is empty",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Real-IP", "192.0.2.2")
				req.RemoteAddr = "198.51.100.1:12345"
				return req
			},
			expectedIP: "192.0.2.2",
		},
		{
			name: "extracts from RemoteAddr when no headers present",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.RemoteAddr = "192.0.2.3:12345"
				return req
			},
			expectedIP: "192.0.2.3",
		},
		{
			name: "handles RemoteAddr without port",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.RemoteAddr = "192.0.2.4"
				return req
			},
			expectedIP: "192.0.2.4",
		},
		{
			name: "prefers X-Forwarded-For over X-Real-IP",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Forwarded-For", "192.0.2.5")
				req.Header.Set("X-Real-IP", "192.0.2.6")
				req.RemoteAddr = "198.51.100.1:12345"
				return req
			},
			expectedIP: "192.0.2.5",
		},
		{
			name: "handles empty X-Forwarded-For and falls back to X-Real-IP",
			setupReq: func() *http.Request {
				req, _ := http.NewRequest("GET", "/", nil)
				req.Header.Set("X-Forwarded-For", "   ")
				req.Header.Set("X-Real-IP", "192.0.2.7")
				req.RemoteAddr = "198.51.100.1:12345"
				return req
			},
			expectedIP: "192.0.2.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			got := ClientIPFromRequest(req)
			if got != tt.expectedIP {
				t.Errorf("ClientIPFromRequest() = %q, want %q", got, tt.expectedIP)
			}
		})
	}
}
