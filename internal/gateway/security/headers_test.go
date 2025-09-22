package security

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	// Create a simple test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with security headers middleware
	handler := SecurityHeaders(testHandler)

	tests := []struct {
		name              string
		requestHeaders    map[string]string
		useTLS            bool
		expectedHeaders   map[string]string
		unexpectedHeaders []string
	}{
		{
			name:           "HTTP request - no HSTS",
			requestHeaders: map[string]string{},
			useTLS:         false,
			expectedHeaders: map[string]string{
				"X-Frame-Options":         "DENY",
				"X-Content-Type-Options":  "nosniff",
				"X-XSS-Protection":        "1; mode=block",
				"Referrer-Policy":         "strict-origin-when-cross-origin",
				"Content-Security-Policy": "default-src 'self'",
			},
			unexpectedHeaders: []string{"Strict-Transport-Security"},
		},
		{
			name: "HTTPS request - includes HSTS",
			requestHeaders: map[string]string{
				"X-Forwarded-Proto": "https",
			},
			useTLS: false,
			expectedHeaders: map[string]string{
				"X-Frame-Options":           "DENY",
				"X-Content-Type-Options":    "nosniff",
				"X-XSS-Protection":          "1; mode=block",
				"Referrer-Policy":           "strict-origin-when-cross-origin",
				"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
			},
		},
		{
			name:           "TLS request - includes HSTS",
			requestHeaders: map[string]string{},
			useTLS:         true,
			expectedHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.useTLS {
				req.TLS = &tls.ConnectionState{}
			}
			for k, v := range tt.requestHeaders {
				req.Header.Set(k, v)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Check expected headers
			for header, expectedValue := range tt.expectedHeaders {
				actualValue := rr.Header().Get(header)
				if actualValue != expectedValue {
					if header == "Content-Security-Policy" && strings.Contains(actualValue, expectedValue) {
						// CSP might have additional directives, just check it contains our expected part
						continue
					}
					t.Errorf("Header %s: expected %q, got %q", header, expectedValue, actualValue)
				}
			}

			// Check unexpected headers are not present
			for _, header := range tt.unexpectedHeaders {
				if value := rr.Header().Get(header); value != "" {
					t.Errorf("Header %s should not be present, but got %q", header, value)
				}
			}
		})
	}
}

func TestContentSecurityPolicy(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name              string
		devMode           string
		expectedStyleSrc  string
		shouldHaveUpgrade bool
	}{
		{
			name:              "Production CSP",
			devMode:           "false",
			expectedStyleSrc:  "style-src 'self'",
			shouldHaveUpgrade: true,
		},
		{
			name:              "Development CSP",
			devMode:           "true",
			expectedStyleSrc:  "style-src 'self' 'unsafe-inline'",
			shouldHaveUpgrade: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set DEV_MODE for this test
			oldDevMode := os.Getenv("DEV_MODE")
			os.Setenv("DEV_MODE", tt.devMode)
			defer os.Setenv("DEV_MODE", oldDevMode)

			handler := SecurityHeaders(testHandler)
			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			csp := rr.Header().Get("Content-Security-Policy")

			// Check common CSP directives
			expectedDirectives := []string{
				"default-src 'self'",
				"script-src 'self'",
				tt.expectedStyleSrc,
				"img-src 'self' data: https:",
				"font-src 'self' data:",
				"connect-src 'self' ws: wss:",
				"frame-ancestors 'none'",
				"base-uri 'self'",
				"form-action 'self'",
			}

			for _, directive := range expectedDirectives {
				if !strings.Contains(csp, directive) {
					t.Errorf("CSP missing directive: %s", directive)
				}
			}

			// Check upgrade-insecure-requests based on environment
			if tt.shouldHaveUpgrade && !strings.Contains(csp, "upgrade-insecure-requests") {
				t.Error("Production CSP should have upgrade-insecure-requests")
			} else if !tt.shouldHaveUpgrade && strings.Contains(csp, "upgrade-insecure-requests") {
				t.Error("Development CSP should not have upgrade-insecure-requests")
			}
		})
	}
}

func TestPermissionsPolicy(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeaders(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	pp := rr.Header().Get("Permissions-Policy")

	// Check that dangerous features are disabled
	disabledFeatures := []string{
		"geolocation=()",
		"microphone=()",
		"camera=()",
		"payment=()",
		"usb=()",
	}

	for _, feature := range disabledFeatures {
		if !strings.Contains(pp, feature) {
			t.Errorf("Permissions-Policy should disable %s", feature)
		}
	}
}

func TestConfigurableSecurityHeaders(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test with custom configuration
	config := &SecurityHeadersConfig{
		FrameOptions:          "SAMEORIGIN",
		ContentTypeOptions:    "nosniff",
		XSSProtection:         "1; mode=block",
		ReferrerPolicy:        "no-referrer",
		ContentSecurityPolicy: "default-src 'none'",
		HSTS:                  "max-age=63072000; includeSubDomains; preload",
		CustomHeaders: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}

	handler := ConfigurableSecurityHeaders(config)(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Check configured headers
	if got := rr.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options: expected SAMEORIGIN, got %s", got)
	}

	if got := rr.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy: expected no-referrer, got %s", got)
	}

	if got := rr.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("CSP: expected custom policy, got %s", got)
	}

	if got := rr.Header().Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("X-Custom-Header: expected custom-value, got %s", got)
	}

	if got := rr.Header().Get("Strict-Transport-Security"); got != "max-age=63072000; includeSubDomains; preload" {
		t.Errorf("HSTS: expected custom value, got %s", got)
	}
}

func TestDisableHSTS(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := &SecurityHeadersConfig{
		DisableHSTS: true,
	}

	handler := ConfigurableSecurityHeaders(config)(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if hsts := rr.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS should be disabled but got: %s", hsts)
	}
}

func TestResponseNotModified(t *testing.T) {
	// Ensure the middleware doesn't interfere with the response body
	expectedBody := "test response body"
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expectedBody))
	})

	handler := SecurityHeaders(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	if body := rr.Body.String(); body != expectedBody {
		t.Errorf("Response body modified: expected %q, got %q", expectedBody, body)
	}
}
