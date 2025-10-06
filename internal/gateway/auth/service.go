package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

// AuthService combines JWT and API token authentication
type AuthService struct {
	jwtManager   *JWTManager
	tokenService *token.Service
	database     *db.Database
	sessions     *SessionManager
}

// AuthUser represents an authenticated user from either JWT or API token
type AuthUser struct {
	Email       string          `json:"email"`
	Name        string          `json:"name"`
	Roles       []string        `json:"roles,omitempty"`       // For JWT users
	Permissions []string        `json:"permissions,omitempty"` // For API token users
	IsAPIToken  bool            `json:"is_api_token"`
	TokenID     *int64          `json:"token_id,omitempty"` // For API tokens
	TokenName   string          `json:"token_name,omitempty"`
	Session     *db.UserSession `json:"-"`
}

// NewAuthService creates a new authentication service
func NewAuthService(jwtManager *JWTManager, tokenService *token.Service, database *db.Database, sessions *SessionManager) *AuthService {
	return &AuthService{
		jwtManager:   jwtManager,
		tokenService: tokenService,
		database:     database,
		sessions:     sessions,
	}
}

// Middleware handles both JWT and API token authentication
func (a *AuthService) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, source, err := a.AuthenticateHTTPRequest(r)
		if err != nil {
			a.writeUnauthorized(w, r, err.Error())
			return
		}

		// Add user to request context and headers for audit logging
		ctx := context.WithValue(r.Context(), UserContextKey, user)
		r = r.WithContext(ctx)

		// Set headers for audit logging
		r.Header.Set("X-User-Name", user.Name)
		r.Header.Set("X-User-Email", user.Email)
		r.Header.Set("X-Auth-Source", source)
		if user.IsAPIToken {
			if user.TokenID != nil {
				r.Header.Set("X-API-Token-ID", fmt.Sprintf("%d", *user.TokenID))
			} else {
				r.Header.Del("X-API-Token-ID")
			}
			tokenName := strings.TrimSpace(user.TokenName)
			if tokenName != "" {
				r.Header.Set("X-API-Token-Name", tokenName)
			} else {
				r.Header.Del("X-API-Token-Name")
			}
		} else {
			r.Header.Del("X-API-Token-ID")
			r.Header.Del("X-API-Token-Name")
		}

		// Note: admin API accepts cookie auth in this environment; endpoints are internal and behind VPN.

		next.ServeHTTP(w, r)
	})
}

// writeUnauthorized centralizes 401 responses and optional debug logging.
func (a *AuthService) writeUnauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	// Non-intrusive hint header + body for diagnostics
	w.Header().Set("X-Error-Reason", reason)
	// Structured debug at log level
	src := r.Header.Get("X-Auth-Source")
	if src == "" {
		src = "none"
	}
	logging.Debugf("[auth:401] %s %s src=%s reason=%s", r.Method, r.URL.Path, src, reason)
	http.Error(w, reason, http.StatusUnauthorized)
}

// AuthenticateHTTPRequest attempts to authenticate the provided request, returning the user and auth source label.
func (a *AuthService) AuthenticateHTTPRequest(r *http.Request) (*AuthUser, string, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			return nil, "header", fmt.Errorf("invalid authorization header format")
		}
		authType := parts[0]
		credentials := parts[1]
		var (
			user *AuthUser
			err  error
		)
		switch authType {
		case "Bearer":
			if strings.HasPrefix(credentials, "rgw_") {
				user, err = a.validateAPIToken(credentials)
			} else if a.sessions != nil {
				user, err = a.validateSessionToken(credentials, r)
				if err != nil {
					user, err = a.validateJWT(credentials)
				}
			} else {
				user, err = a.validateJWT(credentials)
			}
		case "Basic":
			user, err = a.validateBasicAuth(credentials, r)
		default:
			return nil, "header", fmt.Errorf("unsupported authorization type")
		}
		if err != nil {
			return nil, "header", fmt.Errorf("authentication failed: %v", err)
		}
		return user, "header", nil
	}

	if a.sessions != nil {
		if cookie, err := r.Cookie("session_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
			result, err := a.sessions.ValidateSession(cookie.Value, clientIPFromRequest(r), r.UserAgent())
			if err != nil {
				return nil, "cookie", fmt.Errorf("authentication failed: %v", err)
			}
			return &AuthUser{
				Email:      result.User.Email,
				Name:       result.User.Name,
				Roles:      result.User.Roles,
				IsAPIToken: false,
				Session:    result.Session,
			}, "cookie", nil
		}
	}

	return nil, "none", fmt.Errorf("missing authorization")
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (a *AuthService) validateJWT(tokenString string) (*AuthUser, error) {
	claims, err := a.jwtManager.ValidateToken(tokenString)
	if err != nil {
		// Don't expose JWT validation details - just return a generic error
		return nil, fmt.Errorf("authentication failed")
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
			_ = audit.LogDB(a.database, &db.AuditLog{
				UserEmail:      "",
				UserName:       "",
				ActionType:     "auth",
				Action:         "token.validate",
				ResourceType:   "auth",
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
		Name:        user.Name,
		Permissions: apiToken.Permissions,
		IsAPIToken:  true,
		TokenID:     &apiToken.ID,
		TokenName:   apiToken.Name,
	}

	return userResp, nil
}

func (a *AuthService) validateBasicAuth(credentials string, r *http.Request) (*AuthUser, error) {
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

	// If username is "convox", password may be a JWT, session token, or API token
	if username == "convox" {
		if strings.HasPrefix(password, "rgw_") {
			return a.validateAPIToken(password)
		}
		if a.sessions != nil {
			if user, err := a.validateSessionToken(password, r); err == nil {
				return user, nil
			}
		}
		return a.validateJWT(password)
	}

	// Otherwise, password could be an API token or session token tied to a specific account
	if strings.HasPrefix(password, "rgw_") {
		return a.validateAPIToken(password)
	}
	if a.sessions != nil {
		if user, err := a.validateSessionToken(password, r); err == nil {
			return user, nil
		}
	}

	return nil, fmt.Errorf("unsupported authentication method")
}

// GetAuthUser extracts the authenticated user from the request context
func GetAuthUser(ctx context.Context) (*AuthUser, bool) {
	user, ok := ctx.Value(UserContextKey).(*AuthUser)
	return user, ok
}

// GetSessionID extracts the session ID from the request context
func GetSessionID(ctx context.Context) (int64, bool) {
	authUser, ok := ctx.Value(UserContextKey).(*AuthUser)
	if !ok || authUser.IsAPIToken || authUser.Session == nil {
		return 0, false
	}
	return authUser.Session.ID, true
}

// GetUser returns JWT claims for non-API token requests
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

func (a *AuthService) validateSessionToken(token string, r *http.Request) (*AuthUser, error) {
	if a.sessions == nil {
		return nil, fmt.Errorf("session authentication unavailable")
	}
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil, fmt.Errorf("empty session token")
	}
	result, err := a.sessions.ValidateSession(trimmed, clientIPFromRequest(r), r.UserAgent())
	if err != nil {
		return nil, err
	}
	if result == nil || result.User == nil || result.Session == nil {
		return nil, fmt.Errorf("session invalid")
	}
	return &AuthUser{
		Email:      result.User.Email,
		Name:       result.User.Name,
		Roles:      result.User.Roles,
		IsAPIToken: false,
		Session:    result.Session,
	}, nil
}
