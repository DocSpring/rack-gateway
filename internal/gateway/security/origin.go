package security

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// HostValidator validates both Host and Origin headers for HTTP requests
type HostValidator struct {
	allowedDomains []string
	devMode        bool
}

// NewHostValidator creates a new host/origin validator
func NewHostValidator(domain string) *HostValidator {
	domains := []string{}
	if domain != "" {
		domains = append(domains, domain)
	}

	return &HostValidator{
		allowedDomains: domains,
		devMode:        os.Getenv("DEV_MODE") == "true",
	}
}

// Middleware validates Host header (always) and Origin header (when present)
func (hv *HostValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ALWAYS validate Host header (like Rails allowed_hosts)
		host := r.Host
		if host == "" {
			host = r.Header.Get("Host")
		}

		// If no host at all, reject
		if host == "" {
			http.Error(w, "Host header required", http.StatusForbidden)
			return
		}

		if !hv.isAllowedHost(host) {
			http.Error(w, "Host not allowed", http.StatusForbidden)
			return
		}

		// If Origin header is present, it MUST match allowed domains
		origin := r.Header.Get("Origin")
		if origin != "" && !hv.isAllowedOrigin(origin) {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}

		// Set CORS headers for valid origins
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		next.ServeHTTP(w, r)
	})
}

// isAllowedHost checks if a host is allowed
func (hv *HostValidator) isAllowedHost(host string) bool {
	if host == "" {
		return false
	}

	// Strip port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// In dev mode, allow localhost
	if hv.devMode {
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return true
		}

		// Allow configured dev server
		if webDevURL := os.Getenv("WEB_DEV_SERVER_URL"); webDevURL != "" {
			if devURL, err := url.Parse(webDevURL); err == nil {
				if host == devURL.Hostname() {
					return true
				}
			}
		}
	}

	// If no allowed domains configured and not in dev mode, reject everything
	if len(hv.allowedDomains) == 0 && !hv.devMode {
		return false
	}

	// Check allowed domains
	hostLower := strings.ToLower(host)
	for _, allowed := range hv.allowedDomains {
		allowedHost := strings.ToLower(allowed)

		// Exact match
		if hostLower == allowedHost {
			return true
		}

		// Subdomain match (*.example.com)
		if strings.HasPrefix(allowedHost, "*.") {
			suffix := strings.TrimPrefix(allowedHost, "*")
			if strings.HasSuffix(hostLower, suffix) {
				return true
			}
		}
	}

	return false
}

// isAllowedOrigin checks if an origin is allowed
func (hv *HostValidator) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// Use the same host validation logic
	return hv.isAllowedHost(originURL.Host)
}

// HandlePreflight handles CORS preflight requests
func (hv *HostValidator) HandlePreflight(w http.ResponseWriter, r *http.Request) {
	// Validate Host header first
	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}

	if !hv.isAllowedHost(host) {
		http.Error(w, "Host not allowed", http.StatusForbidden)
		return
	}

	// Then validate Origin
	origin := r.Header.Get("Origin")
	if origin == "" || !hv.isAllowedOrigin(origin) {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return
	}

	// Set CORS headers for preflight
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, X-User-Email, X-Request-ID")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Max-Age", "3600")

	w.WriteHeader(http.StatusNoContent)
}
