package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
)

// AuthService combines JWT and API token authentication
type AuthService struct {
	jwtManager   *JWTManager
	tokenService *token.Service
	database     *db.Database
}

// AuthUser represents an authenticated user from either JWT or API token
type AuthUser struct {
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Roles       []string `json:"roles,omitempty"`       // For JWT users
	Permissions []string `json:"permissions,omitempty"` // For API token users
	IsAPIToken  bool     `json:"is_api_token"`
	TokenID     *int64   `json:"token_id,omitempty"` // For API tokens
}

// NewAuthService creates a new authentication service
func NewAuthService(jwtManager *JWTManager, tokenService *token.Service, database *db.Database) *AuthService {
	return &AuthService{
		jwtManager:   jwtManager,
		tokenService: tokenService,
		database:     database,
	}
}

// Middleware handles both JWT and API token authentication
func (a *AuthService) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			if c, err := r.Cookie("gateway_token"); err == nil && c.Value != "" {
				authHeader = "Bearer " + c.Value
			}
		}
		if authHeader == "" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		authType := parts[0]
		credentials := parts[1]

		var user *AuthUser
		var err error

		switch authType {
		case "Bearer":
			// Check if it's an API token (starts with cgw_) or JWT
			if strings.HasPrefix(credentials, "cgw_") {
				user, err = a.validateAPIToken(credentials)
			} else {
				user, err = a.validateJWT(credentials)
			}
		case "Basic":
			// Convox CLI uses Basic auth - decode and check password field
			user, err = a.validateBasicAuth(credentials)
		default:
			http.Error(w, "unsupported authorization type", http.StatusUnauthorized)
			return
		}

		if err != nil {
			http.Error(w, fmt.Sprintf("authentication failed: %v", err), http.StatusUnauthorized)
			return
		}

		// Add user to request context and headers for audit logging
		ctx := context.WithValue(r.Context(), UserContextKey, user)
		r = r.WithContext(ctx)

		// Set headers for audit logging
		r.Header.Set("X-User-Name", user.Name)
		r.Header.Set("X-User-Email", user.Email)

		next.ServeHTTP(w, r)
	})
}

func (a *AuthService) validateJWT(tokenString string) (*AuthUser, error) {
	claims, err := a.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT: %w", err)
	}

	// Get user roles from database
	user, err := a.database.GetUser(claims.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if user.Suspended {
		return nil, fmt.Errorf("user is suspended")
	}

	return &AuthUser{
		Email:      claims.Email,
		Name:       claims.Name,
		Roles:      user.Roles,
		IsAPIToken: false,
	}, nil
}

func (a *AuthService) validateAPIToken(tokenString string) (*AuthUser, error) {
	apiToken, err := a.tokenService.ValidateAPIToken(tokenString)
	if err != nil {
		// Audit failed token validation (do not log raw token)
		if a.database != nil {
			_ = a.database.CreateAuditLog(&db.AuditLog{
				UserEmail:      "",
				UserName:       "",
				ActionType:     "auth",
				Action:         "token.validate",
				Resource:       "api_token",
				Details:        "{\"result\":\"error\"}",
				IPAddress:      "",
				UserAgent:      "",
				Status:         "error",
				ResponseTimeMs: 0,
			})
		}
		return nil, fmt.Errorf("invalid API token: %w", err)
	}

	// Get the user who owns this token
	user, err := a.database.GetUserByID(apiToken.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token owner: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("token owner not found")
	}
	if user.Suspended {
		return nil, fmt.Errorf("token owner is suspended")
	}

	userResp := &AuthUser{
		Email:       user.Email,
		Name:        user.Name + " (API)",
		Permissions: apiToken.Permissions,
		IsAPIToken:  true,
		TokenID:     &apiToken.ID,
	}

	// Audit successful token validation / usage (no sensitive values; include token id)
	if a.database != nil {
		_ = a.database.CreateAuditLog(&db.AuditLog{
			UserEmail:      user.Email,
			UserName:       user.Name,
			ActionType:     "auth",
			Action:         "token.validate",
			Resource:       fmt.Sprintf("token_id:%d", apiToken.ID),
			Details:        "{\"result\":\"success\"}",
			IPAddress:      "",
			UserAgent:      "",
			Status:         "success",
			ResponseTimeMs: 0,
		})
	}

	return userResp, nil
}

func (a *AuthService) validateBasicAuth(credentials string) (*AuthUser, error) {
	// For Basic auth, try to decode and check both username:password formats
	decoded, err := decodeBasicAuth(credentials)
	if err != nil {
		return nil, fmt.Errorf("invalid basic auth: %w", err)
	}

	parts := strings.SplitN(decoded, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid basic auth format")
	}

	username := parts[0]
	password := parts[1]

	// If username is "convox", password should be a JWT (Convox CLI behavior)
	if username == "convox" {
		return a.validateJWT(password)
	}

	// Otherwise, password could be an API token
	if strings.HasPrefix(password, "cgw_") {
		return a.validateAPIToken(password)
	}

	return nil, fmt.Errorf("unsupported authentication method")
}

// GetUser extracts the authenticated user from the request context
func GetAuthUser(ctx context.Context) (*AuthUser, bool) {
	user, ok := ctx.Value(UserContextKey).(*AuthUser)
	return user, ok
}

// GetUser maintains backward compatibility for JWT-based claims
func GetUser(ctx context.Context) (*Claims, bool) {
	authUser, ok := ctx.Value(UserContextKey).(*AuthUser)
	if !ok || authUser.IsAPIToken {
		return nil, false
	}

	return &Claims{
		Email: authUser.Email,
		Name:  authUser.Name,
	}, true
}

// decodeBasicAuth decodes a basic auth credentials string
func decodeBasicAuth(credentials string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}
	return string(decoded), nil
}
