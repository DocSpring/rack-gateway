package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostValidation(t *testing.T) {
	hv := NewHostValidator("example.com")

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler := hv.Middleware(testHandler)

	tests := []struct {
		name           string
		host           string
		origin         string
		expectedStatus int
	}{
		{
			name:           "valid host, no origin",
			host:           "example.com",
			origin:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid host with port, no origin",
			host:           "example.com:443",
			origin:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid host",
			host:           "evil.com",
			origin:         "",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "valid host, valid origin",
			host:           "example.com",
			origin:         "https://example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid host, invalid origin",
			host:           "example.com",
			origin:         "https://evil.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "empty host",
			host:           "",
			origin:         "",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			// Always set the host - if empty, we expect rejection
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			// Check CORS headers when origin is valid
			if tt.expectedStatus == http.StatusOK && tt.origin != "" {
				if corsOrigin := rr.Header().Get("Access-Control-Allow-Origin"); corsOrigin != tt.origin {
					t.Errorf("expected CORS origin %s, got %s", tt.origin, corsOrigin)
				}
				if credentials := rr.Header().Get("Access-Control-Allow-Credentials"); credentials != "true" {
					t.Errorf("expected CORS credentials true, got %s", credentials)
				}
			}
		})
	}
}

func TestHostValidationDevMode(t *testing.T) {
	// Set dev mode
	t.Setenv("DEV_MODE", "true")
	t.Setenv("WEB_DEV_SERVER_URL", "http://localhost:5223")

	hv := NewHostValidator("example.com")

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := hv.Middleware(testHandler)

	tests := []struct {
		name           string
		host           string
		origin         string
		expectedStatus int
	}{
		{
			name:           "localhost allowed in dev",
			host:           "localhost:8447",
			origin:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "127.0.0.1 allowed in dev",
			host:           "127.0.0.1:8447",
			origin:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "dev server URL allowed",
			host:           "localhost:5223",
			origin:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "localhost origin allowed in dev",
			host:           "localhost:8447",
			origin:         "http://localhost:5223",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "production domain still allowed",
			host:           "example.com",
			origin:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "random domain still rejected",
			host:           "evil.com",
			origin:         "",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestWildcardDomains(t *testing.T) {
	hv := NewHostValidator("*.example.com")

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := hv.Middleware(testHandler)

	tests := []struct {
		name           string
		host           string
		expectedStatus int
	}{
		{
			name:           "subdomain matches",
			host:           "api.example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "nested subdomain matches",
			host:           "v2.api.example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "different domain rejected",
			host:           "api.evil.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "partial match rejected",
			host:           "example.com.evil.com",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Host = tt.host

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestHandlePreflight(t *testing.T) {
	hv := NewHostValidator("example.com")

	tests := []struct {
		name           string
		host           string
		origin         string
		expectedStatus int
		expectCORS     bool
	}{
		{
			name:           "valid host and origin",
			host:           "example.com",
			origin:         "https://example.com",
			expectedStatus: http.StatusNoContent,
			expectCORS:     true,
		},
		{
			name:           "invalid host",
			host:           "evil.com",
			origin:         "https://example.com",
			expectedStatus: http.StatusForbidden,
			expectCORS:     false,
		},
		{
			name:           "valid host, invalid origin",
			host:           "example.com",
			origin:         "https://evil.com",
			expectedStatus: http.StatusForbidden,
			expectCORS:     false,
		},
		{
			name:           "no origin header",
			host:           "example.com",
			origin:         "",
			expectedStatus: http.StatusForbidden,
			expectCORS:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("OPTIONS", "/test", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rr := httptest.NewRecorder()
			hv.HandlePreflight(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectCORS {
				// Check all CORS headers
				if corsOrigin := rr.Header().Get("Access-Control-Allow-Origin"); corsOrigin != tt.origin {
					t.Errorf("expected CORS origin %s, got %s", tt.origin, corsOrigin)
				}
				if methods := rr.Header().Get("Access-Control-Allow-Methods"); methods == "" {
					t.Error("missing Access-Control-Allow-Methods header")
				}
				if headers := rr.Header().Get("Access-Control-Allow-Headers"); headers == "" {
					t.Error("missing Access-Control-Allow-Headers header")
				}
				if credentials := rr.Header().Get("Access-Control-Allow-Credentials"); credentials != "true" {
					t.Error("missing Access-Control-Allow-Credentials header")
				}
				if maxAge := rr.Header().Get("Access-Control-Max-Age"); maxAge != "3600" {
					t.Error("missing Access-Control-Max-Age header")
				}
			}
		})
	}
}
