package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
)

type contextKey string

const UserContextKey contextKey = "user"

func (m *JWTManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		var token string
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		switch parts[0] {
		case "Bearer":
			// Standard Bearer token
			token = parts[1]
		case "Basic":
			// Convox CLI sends Basic auth where username is "convox" and password is JWT
			decoded, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				http.Error(w, "invalid basic auth encoding", http.StatusUnauthorized)
				return
			}

			credentials := string(decoded)
			colonIndex := strings.Index(credentials, ":")
			if colonIndex < 0 {
				http.Error(w, "invalid basic auth format", http.StatusUnauthorized)
				return
			}

			// Extract JWT from password field (username should be "convox")
			token = credentials[colonIndex+1:]
		default:
			http.Error(w, "unsupported authorization type", http.StatusUnauthorized)
			return
		}

		claims, err := m.ValidateToken(token)
		if err != nil {
			http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUser moved to service.go

func OptionalAuth(jwtManager *JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && parts[0] == "Bearer" {
					if claims, err := jwtManager.ValidateToken(parts[1]); err == nil {
						ctx := context.WithValue(r.Context(), UserContextKey, claims)
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
