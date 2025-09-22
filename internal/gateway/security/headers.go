package security

import (
	"net/http"
	"os"
	"strings"
)

// SecurityHeaders middleware adds important security headers to all responses
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// X-Frame-Options prevents clickjacking attacks
		// DENY is strictest, could use SAMEORIGIN if needed for embedded scenarios
		w.Header().Set("X-Frame-Options", "DENY")

		// X-Content-Type-Options prevents MIME type sniffing
		// This is already set in some places but we ensure it's always present
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// X-XSS-Protection for older browsers (modern browsers ignore this in favor of CSP)
		// 1; mode=block enables XSS filter and blocks the page if attack detected
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer-Policy controls how much referrer info is sent
		// strict-origin-when-cross-origin is a good balance of privacy and functionality
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions-Policy (formerly Feature-Policy) restricts browser features
		// Disable features that aren't needed for security
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=(), magnetometer=(), accelerometer=(), gyroscope=()")

		// Content-Security-Policy helps prevent XSS and other injection attacks
		w.Header().Set("Content-Security-Policy", getCSP())

		// Strict-Transport-Security (HSTS) forces HTTPS
		// Only set this in production or when using HTTPS
		// max-age=31536000 is 1 year, includeSubDomains applies to all subdomains
		// Note: Be careful with preload - it's permanent for the domain
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersConfig allows customization of security headers
type SecurityHeadersConfig struct {
	FrameOptions          string            // X-Frame-Options value
	ContentTypeOptions    string            // X-Content-Type-Options value
	XSSProtection         string            // X-XSS-Protection value
	ReferrerPolicy        string            // Referrer-Policy value
	PermissionsPolicy     string            // Permissions-Policy value
	ContentSecurityPolicy string            // Content-Security-Policy value
	HSTS                  string            // Strict-Transport-Security value
	DisableHSTS           bool              // Disable HSTS header
	CustomHeaders         map[string]string // Additional custom headers
}

// ConfigurableSecurityHeaders middleware with configuration options
func ConfigurableSecurityHeaders(config *SecurityHeadersConfig) func(http.Handler) http.Handler {
	// Set defaults if not provided
	if config == nil {
		config = &SecurityHeadersConfig{}
	}

	if config.FrameOptions == "" {
		config.FrameOptions = "DENY"
	}
	if config.ContentTypeOptions == "" {
		config.ContentTypeOptions = "nosniff"
	}
	if config.XSSProtection == "" {
		config.XSSProtection = "1; mode=block"
	}
	if config.ReferrerPolicy == "" {
		config.ReferrerPolicy = "strict-origin-when-cross-origin"
	}
	if config.PermissionsPolicy == "" {
		config.PermissionsPolicy = "geolocation=(), microphone=(), camera=(), payment=(), usb=()"
	}
	if config.ContentSecurityPolicy == "" {
		config.ContentSecurityPolicy = getCSP()
	}
	if config.HSTS == "" && !config.DisableHSTS {
		config.HSTS = "max-age=31536000; includeSubDomains"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Apply configured headers
			w.Header().Set("X-Frame-Options", config.FrameOptions)
			w.Header().Set("X-Content-Type-Options", config.ContentTypeOptions)
			w.Header().Set("X-XSS-Protection", config.XSSProtection)
			w.Header().Set("Referrer-Policy", config.ReferrerPolicy)
			w.Header().Set("Permissions-Policy", config.PermissionsPolicy)
			w.Header().Set("Content-Security-Policy", config.ContentSecurityPolicy)

			// HSTS only on HTTPS connections
			if !config.DisableHSTS && config.HSTS != "" {
				if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
					w.Header().Set("Strict-Transport-Security", config.HSTS)
				}
			}

			// Apply any custom headers
			for key, value := range config.CustomHeaders {
				w.Header().Set(key, value)
			}

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}

// getCSP returns the Content-Security-Policy based on environment
func getCSP() string {
	isDev := os.Getenv("DEV_MODE") == "true"

	// Style source depends on environment
	styleSrc := "style-src 'self'"
	if isDev {
		styleSrc = "style-src 'self' 'unsafe-inline'" // Vite HMR needs inline styles
	}

	// Connect source depends on environment
	connectSrc := "connect-src 'self' ws: wss:"
	if isDev {
		// In dev mode, allow connections to localhost ports for Vite dev server
		connectSrc = "connect-src 'self' ws: wss: http://localhost:* https://localhost:*"
	}

	// Base CSP directives
	csp := []string{
		"default-src 'self'",
		"script-src 'self'",
		styleSrc,
		"img-src 'self' data: https:",
		"font-src 'self' data:",
		connectSrc,
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}

	// Production adds upgrade-insecure-requests
	if !isDev {
		csp = append(csp, "upgrade-insecure-requests")
	}

	return strings.Join(csp, "; ")
}
