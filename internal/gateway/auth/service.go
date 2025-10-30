package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

type contextKey string

const (
	// UserContextKey is the context key used to store authenticated user information
	UserContextKey contextKey = "user"
	userRecordKey  contextKey = "user_record"
)

// Service handles session and API token authentication
type Service struct {
	tokenService *token.Service
	database     *db.Database
	sessions     *SessionManager
}

// User represents an authenticated user from either session or API token
type User struct {
	Email              string          `json:"email"`
	Name               string          `json:"name"`
	Roles              []string        `json:"roles,omitempty"`       // For session users
	Permissions        []string        `json:"permissions,omitempty"` // For API token users
	IsAPIToken         bool            `json:"is_api_token"`
	TokenID            *int64          `json:"token_id,omitempty"` // For API tokens
	TokenName          string          `json:"token_name,omitempty"`
	Session            *db.UserSession `json:"-"`
	MFAType            string          `json:"-"` // "totp" or "webauthn" - inline MFA provided with request
	MFAValue           string          `json:"-"` // The MFA verification data (TOTP code or WebAuthn assertion)
	Suspended          bool            `json:"-"`
	MFAEnrolled        bool            `json:"-"`
	PreferredMFAMethod *string         `json:"-"`
	LockedAt           *time.Time      `json:"-"`
	LockedReason       string          `json:"-"`
	LockedByUserID     *int64          `json:"-"`
	LockedByEmail      string          `json:"-"`
	LockedByName       string          `json:"-"`
	DBUser             *db.User        `json:"-"`
}

// NewAuthService creates a new authentication service
func NewAuthService(tokenService *token.Service, database *db.Database, sessions *SessionManager) *Service {
	return &Service{
		tokenService: tokenService,
		database:     database,
		sessions:     sessions,
	}
}

// Middleware handles session token and API token authentication
func (a *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, source, err := a.AuthenticateHTTPRequest(r)
		if err != nil {
			a.writeUnauthorized(w, r, err.Error())
			return
		}

		r = a.withUserContext(r, user)
		a.setAuthHeaders(r, user, source)

		next.ServeHTTP(w, r)
	})
}

func (a *Service) withUserContext(r *http.Request, user *User) *http.Request {
	ctx := context.WithValue(r.Context(), UserContextKey, user)
	if user != nil && user.DBUser != nil {
		ctx = context.WithValue(ctx, userRecordKey, user.DBUser)
	}
	return r.WithContext(ctx)
}

func (a *Service) setAuthHeaders(r *http.Request, user *User, source string) {
	r.Header.Set("X-User-Name", user.Name)
	r.Header.Set("X-User-Email", user.Email)
	r.Header.Set("X-Auth-Source", source)

	if !user.IsAPIToken {
		r.Header.Del("X-API-Token-ID")
		r.Header.Del("X-API-Token-Name")
		return
	}

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
}

// writeUnauthorized centralizes 401 responses and optional debug logging.
func (a *Service) writeUnauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	// Non-intrusive hint header + body for diagnostics
	w.Header().Set("X-Error-Reason", reason)
	// Structured debug at log level
	src := r.Header.Get("X-Auth-Source")
	if src == "" {
		src = "none"
	}
	if logging.TopicEnabled(logging.TopicAuth) {
		logging.DebugTopicf(logging.TopicAuth, "[auth:401] %s %s src=%s reason=%s", r.Method, r.URL.Path, src, reason)
	}
	http.Error(w, reason, http.StatusUnauthorized)
}

// AuthenticateHTTPRequest attempts to authenticate the provided request, returning the user and auth source label.
func (a *Service) AuthenticateHTTPRequest(r *http.Request) (*User, string, error) {
	if user, source, err := a.authenticateFromHeader(r); user != nil || err != nil {
		if err != nil {
			return nil, source, err
		}
		a.applyHeaderMFA(r, user)
		return user, source, nil
	}

	if user, source, err := a.authenticateFromCookie(r); user != nil || err != nil {
		if err != nil {
			return nil, source, err
		}
		a.applyHeaderMFA(r, user)
		return user, source, nil
	}

	return nil, "none", fmt.Errorf("missing authorization")
}

func (a *Service) authenticateFromHeader(r *http.Request) (*User, string, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return nil, "", nil
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return nil, "header", fmt.Errorf("invalid authorization header format")
	}

	authType := parts[0]
	credentials := parts[1]

	switch authType {
	case "Bearer":
		user, err := a.authenticateBearer(credentials, r)
		if err != nil {
			return nil, "header", fmt.Errorf("authentication failed: %v", err)
		}
		return user, "header", nil
	case "Basic":
		user, err := a.validateBasicAuth(credentials, r)
		if err != nil {
			return nil, "header", fmt.Errorf("authentication failed: %v", err)
		}
		return user, "header", nil
	default:
		return nil, "header", fmt.Errorf("unsupported authorization type")
	}
}

func (a *Service) authenticateBearer(credentials string, r *http.Request) (*User, error) {
	if strings.HasPrefix(credentials, "rgw_") {
		return a.validateAPIToken(credentials)
	}

	sessionToken, mfaType, mfaValue := parseInlineMFA(credentials)
	user, err := a.validateSessionToken(sessionToken, r)
	if err != nil {
		return nil, err
	}
	if mfaType != "" && mfaValue != "" {
		user.MFAType = mfaType
		user.MFAValue = mfaValue
	}
	return user, nil
}

func (a *Service) authenticateFromCookie(r *http.Request) (*User, string, error) {
	cookie, err := r.Cookie("session_token")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil, "", nil
	}

	result, err := a.sessions.ValidateSession(cookie.Value, netutil.ClientIPFromRequest(r), r.UserAgent())
	if err != nil {
		return nil, "cookie", fmt.Errorf("authentication failed: %v", err)
	}

	user := newAuthUserFromSessionResult(result)
	if user == nil {
		return nil, "cookie", fmt.Errorf("authentication failed: invalid session")
	}

	return user, "cookie", nil
}

func (a *Service) applyHeaderMFA(r *http.Request, user *User) {
	if totpCode := strings.TrimSpace(r.Header.Get("X-Mfa-Totp")); totpCode != "" {
		user.MFAType = "totp"
		user.MFAValue = totpCode
		return
	}
	if webauthnData := strings.TrimSpace(r.Header.Get("X-MFA-WebAuthn")); webauthnData != "" {
		user.MFAType = "webauthn"
		user.MFAValue = webauthnData
	}
}

func (a *Service) validateAPIToken(tokenString string) (*User, error) {
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

	userResp := &User{
		Email:              user.Email,
		Name:               user.Name,
		Permissions:        append([]string(nil), apiToken.Permissions...),
		IsAPIToken:         true,
		TokenID:            &apiToken.ID,
		TokenName:          apiToken.Name,
		Suspended:          user.Suspended,
		MFAEnrolled:        user.MFAEnrolled,
		PreferredMFAMethod: user.PreferredMFAMethod,
		LockedAt:           user.LockedAt,
		LockedReason:       user.LockedReason,
		LockedByUserID:     user.LockedByUserID,
		LockedByEmail:      user.LockedByEmail,
		LockedByName:       user.LockedByName,
		DBUser:             user,
	}

	return userResp, nil
}

func (a *Service) validateBasicAuth(credentials string, r *http.Request) (*User, error) {
	// For Basic auth, try to decode and check both username:password formats
	decoded, err := decodeBasicAuth(credentials)
	if err != nil {
		return nil, fmt.Errorf("invalid basic auth: %w", err)
	}

	parts := strings.SplitN(decoded, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid basic auth format")
	}

	password := parts[1]

	// Parse password field for optional inline MFA data
	authToken, mfaType, mfaValue := parseInlineMFA(password)

	var user *User

	// authToken may be a session token or API token
	if strings.HasPrefix(authToken, "rgw_") {
		user, err = a.validateAPIToken(authToken)
	} else {
		user, err = a.validateSessionToken(authToken, r)
	}

	if err != nil {
		return nil, err
	}

	// Attach MFA data to the user if provided
	if mfaType != "" && mfaValue != "" {
		user.MFAType = mfaType
		user.MFAValue = mfaValue
	}

	return user, nil
}

// GetAuthUser extracts the authenticated user from the request context
func GetAuthUser(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(UserContextKey).(*User)
	return user, ok
}

// GetAuthUserRecord retrieves the loaded db.User from context when available.
func GetAuthUserRecord(ctx context.Context) *db.User {
	if user, ok := ctx.Value(UserContextKey).(*User); ok && user != nil && user.DBUser != nil {
		return user.DBUser
	}
	if v := ctx.Value(userRecordKey); v != nil {
		if dbUser, ok := v.(*db.User); ok {
			return dbUser
		}
	}
	return nil
}

// GetSessionID extracts the session ID from the request context
func GetSessionID(ctx context.Context) (int64, bool) {
	authUser, ok := ctx.Value(UserContextKey).(*User)
	if !ok || authUser.IsAPIToken || authUser.Session == nil {
		return 0, false
	}
	return authUser.Session.ID, true
}

// decodeBasicAuth decodes a basic auth credentials string
func decodeBasicAuth(credentials string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}
	return string(decoded), nil
}

// parseInlineMFA parses a token string for optional inline MFA data
// Format: token.mfa_type.mfa_value (e.g., "session123.totp.123456" or "session123.webauthn.base64data")
// Returns: (token, mfaType, mfaValue)
func parseInlineMFA(tokenString string) (string, string, string) {
	parts := strings.SplitN(tokenString, ".", 3)
	if len(parts) == 3 {
		// MFA data present
		return parts[0], parts[1], parts[2]
	}
	// No MFA data, just the token
	return tokenString, "", ""
}

func newAuthUserFromSessionResult(result *SessionValidationResult) *User {
	if result == nil || result.User == nil {
		return nil
	}
	authUser := &User{
		Email:              result.User.Email,
		Name:               result.User.Name,
		Roles:              append([]string(nil), result.User.Roles...),
		IsAPIToken:         false,
		Session:            result.Session,
		Suspended:          result.User.Suspended,
		MFAEnrolled:        result.User.MFAEnrolled,
		PreferredMFAMethod: result.User.PreferredMFAMethod,
		LockedAt:           result.User.LockedAt,
		LockedReason:       result.User.LockedReason,
		LockedByUserID:     result.User.LockedByUserID,
		LockedByEmail:      result.User.LockedByEmail,
		LockedByName:       result.User.LockedByName,
		DBUser:             result.User,
	}
	if result.Session != nil {
		result.Session.UserID = result.User.ID
	}
	return authUser
}

func (a *Service) validateSessionToken(sessionToken string, r *http.Request) (*User, error) {
	trimmed := strings.TrimSpace(sessionToken)
	if trimmed == "" {
		return nil, fmt.Errorf("empty session token")
	}
	result, err := a.sessions.ValidateSession(trimmed, netutil.ClientIPFromRequest(r), r.UserAgent())
	if err != nil {
		return nil, err
	}
	authUser := newAuthUserFromSessionResult(result)
	if authUser == nil || authUser.Session == nil {
		return nil, fmt.Errorf("session invalid")
	}
	return authUser, nil
}
