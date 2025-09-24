package auth

import (
	"net/http"
	"strings"
)

// CLIOnlyMiddleware wraps the standard auth middleware but rejects cookie-based auth.
// This ensures that proxy routes can only be accessed by the CLI (with Authorization header),
// not by browsers (with cookies), preventing CSRF attacks on the Convox API.
func (a *AuthService) CLIOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
			a.writeUnauthorized(w, r, "browser session cookies are not permitted for CLI routes")
			return
		}

		// Check if request has Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			a.writeUnauthorized(w, r, "CLI authentication required - provide Authorization header")
			return
		}

		// Ensure it's Bearer or Basic auth (not cookie-based)
		if !strings.HasPrefix(authHeader, "Bearer ") && !strings.HasPrefix(authHeader, "Basic ") {
			a.writeUnauthorized(w, r, "invalid authorization type for CLI access")
			return
		}

		// Now use the standard auth middleware to validate the token
		a.Middleware(next).ServeHTTP(w, r)
	})
}
