package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

const (
	sessionTokenByteLength    = 32
	defaultSessionIdleTimeout = 5 * time.Minute
	cliDefaultAbsoluteTTL     = 90 * 24 * time.Hour
)

// SessionManager manages user sessions including creation, validation, and revocation.
type SessionManager struct {
	db     *db.Database
	secret []byte
	ttl    time.Duration
}

// SessionMetadata contains metadata for creating a new session.
type SessionMetadata struct {
	Channel        string
	DeviceID       string
	DeviceName     string
	DeviceMetadata map[string]interface{}
	IPAddress      string
	UserAgent      string
	Extra          map[string]interface{}
	TTLOverride    time.Duration
}

// SessionValidationResult contains the result of validating a session token.
type SessionValidationResult struct {
	Session *db.UserSession
	User    *db.User
}

// NewSessionManager creates a new session manager with the given configuration.
func NewSessionManager(database *db.Database, secret string, ttl time.Duration) *SessionManager {
	timeout := ttl
	if timeout <= 0 {
		timeout = defaultSessionIdleTimeout
	}
	return &SessionManager{
		db:     database,
		secret: []byte(secret),
		ttl:    timeout,
	}
}

// TTL returns the default session time-to-live duration.
func (m *SessionManager) TTL() time.Duration {
	if m == nil {
		return 0
	}
	return m.ttl
}

// CreateSession creates a new session for the given user with metadata.
func (m *SessionManager) CreateSession(user *db.User, meta SessionMetadata) (string, *db.UserSession, error) {
	if m == nil {
		return "", nil, fmt.Errorf("session manager not initialized")
	}
	if user == nil || user.ID == 0 {
		return "", nil, fmt.Errorf("invalid user for session creation")
	}

	tokenBytes := make([]byte, sessionTokenByteLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	sessionToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenHash := hashSessionToken(sessionToken)

	now := time.Now()
	ttl := m.ttl
	if meta.TTLOverride > 0 {
		ttl = meta.TTLOverride
	}
	expiresAt := now.Add(ttl)

	chanVal := strings.TrimSpace(meta.Channel)
	if chanVal == "" {
		chanVal = "web"
	}

	extra := make(map[string]interface{})
	for k, v := range meta.Extra {
		extra[k] = v
	}
	extra["ttl_seconds"] = ttl.Seconds()

	session, err := m.db.CreateUserSession(
		user.ID,
		tokenHash,
		expiresAt,
		chanVal,
		meta.DeviceID,
		meta.DeviceName,
		meta.IPAddress,
		meta.UserAgent,
		extra,
		meta.DeviceMetadata,
	)
	if err != nil {
		return "", nil, err
	}

	session.LastSeenAt = now
	session.ExpiresAt = expiresAt
	session.Channel = chanVal
	session.DeviceID = strings.TrimSpace(meta.DeviceID)
	session.DeviceName = strings.TrimSpace(meta.DeviceName)
	return sessionToken, session, nil
}

// ValidateSession validates a session token and returns the session and user if valid.
func (m *SessionManager) ValidateSession(sessionToken, ipAddress, userAgent string) (*SessionValidationResult, error) {
	if m == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	trimmed := strings.TrimSpace(sessionToken)
	if trimmed == "" {
		return nil, fmt.Errorf("empty session token")
	}

	session, user, err := m.loadSessionAndUser(trimmed)
	if err != nil {
		return nil, err
	}

	m.logSessionDebugInfo(session, user)

	if err := m.validateSessionState(session, user); err != nil {
		return nil, err
	}

	ttl := m.sessionTTLFor(session)
	m.refreshSessionActivity(session, ipAddress, userAgent, ttl)

	return &SessionValidationResult{Session: session, User: user}, nil
}

func (m *SessionManager) loadSessionAndUser(sessionToken string) (*db.UserSession, *db.User, error) {
	tokenHash := hashSessionToken(sessionToken)
	session, user, err := m.db.GetUserSessionWithUserByHash(tokenHash)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, fmt.Errorf("session not found")
	}
	return session, user, nil
}

func (m *SessionManager) logSessionDebugInfo(session *db.UserSession, user *db.User) {
	var stepUpAtStr string
	if session.RecentStepUpAt != nil {
		stepUpAtStr = session.RecentStepUpAt.Format(time.RFC3339)
	} else {
		stepUpAtStr = "nil"
	}
	gtwlog.DebugTopicf(
		gtwlog.TopicMFAStepUp,
		"validate_session_loaded session_id=%d user_email=%q recent_step_up_at=%q",
		session.ID,
		user.Email,
		stepUpAtStr,
	)
}

func (m *SessionManager) validateSessionState(session *db.UserSession, user *db.User) error {
	if session.RevokedAt != nil {
		return fmt.Errorf("session revoked")
	}

	now := time.Now()
	if session.ExpiresAt.Before(now) {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return fmt.Errorf("session expired")
	}

	ttl := m.sessionTTLFor(session)
	if ttl > 0 && session.LastSeenAt.Add(ttl).Before(now) {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return fmt.Errorf("session expired")
	}

	return m.validateUserState(session.ID, user)
}

func (m *SessionManager) validateUserState(sessionID int64, user *db.User) error {
	if user == nil {
		_, _ = m.db.RevokeUserSession(sessionID, nil)
		return fmt.Errorf("session user missing")
	}
	if user.Suspended {
		_, _ = m.db.RevokeUserSession(sessionID, nil)
		return fmt.Errorf("user suspended")
	}
	if user.LockedAt != nil {
		_, _ = m.db.RevokeUserSession(sessionID, nil)
		return fmt.Errorf("user locked")
	}
	return nil
}

func (m *SessionManager) refreshSessionActivity(
	session *db.UserSession,
	ipAddress, userAgent string,
	ttl time.Duration,
) {
	now := time.Now()
	err := m.db.TouchUserSession(session.ID, ipAddress, userAgent, now, now.Add(ttl))
	if err == nil {
		session.LastSeenAt = now
		session.ExpiresAt = now.Add(ttl)
		if trimmedIP := strings.TrimSpace(ipAddress); trimmedIP != "" {
			session.IPAddress = trimmedIP
		}
		if trimmedUA := strings.TrimSpace(userAgent); trimmedUA != "" {
			session.UserAgent = trimmedUA
		}
	}
}

// RevokeByToken revokes a session using its token string.
func (m *SessionManager) RevokeByToken(sessionToken string, revokedBy *int64) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("session manager not initialized")
	}
	trimmed := strings.TrimSpace(sessionToken)
	if trimmed == "" {
		return false, fmt.Errorf("empty session token")
	}
	return m.db.RevokeUserSessionByHash(hashSessionToken(trimmed), revokedBy)
}

// RevokeByID revokes a session using its database ID.
func (m *SessionManager) RevokeByID(id int64, revokedBy *int64) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("session manager not initialized")
	}
	if id <= 0 {
		return false, fmt.Errorf("invalid session id")
	}
	return m.db.RevokeUserSession(id, revokedBy)
}

// RevokeAllForUser revokes all active sessions for the given user.
func (m *SessionManager) RevokeAllForUser(userID int64, revokedBy *int64) (int64, error) {
	if m == nil {
		return 0, fmt.Errorf("session manager not initialized")
	}
	if userID <= 0 {
		return 0, fmt.Errorf("invalid user id")
	}
	return m.db.RevokeAllUserSessions(userID, revokedBy)
}

// ListActiveForUser returns all active sessions for the given user.
func (m *SessionManager) ListActiveForUser(userID int64) ([]*db.UserSession, error) {
	if m == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	return m.db.ListActiveSessionsByUser(userID)
}

// UpdateSessionMFAVerified marks the session as MFA verified.
func (m *SessionManager) UpdateSessionMFAVerified(sessionID int64, verifiedAt time.Time, trustedDeviceID *int64) error {
	if m == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return m.db.UpdateSessionMFAVerified(sessionID, verifiedAt, trustedDeviceID)
}

// UpdateSessionRecentStepUp updates the recent step-up timestamp for the session.
func (m *SessionManager) UpdateSessionRecentStepUp(sessionID int64, when time.Time) error {
	if m == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return m.db.UpdateSessionRecentStepUp(sessionID, when)
}

// AttachTrustedDeviceToSession attaches a trusted device to the session.
func (m *SessionManager) AttachTrustedDeviceToSession(sessionID int64, trustedDeviceID int64) error {
	if m == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return m.db.AttachTrustedDeviceToSession(sessionID, trustedDeviceID)
}

// DeriveCSRFToken derives a CSRF token from a session token using HMAC.
func (m *SessionManager) DeriveCSRFToken(sessionToken string) (string, error) {
	trimmed := strings.TrimSpace(sessionToken)
	if trimmed == "" || len(m.secret) == 0 {
		return "", fmt.Errorf("missing data for CSRF derivation")
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte("csrf|"))
	mac.Write([]byte(trimmed))
	sum := mac.Sum(nil)
	return base64.RawStdEncoding.EncodeToString(sum), nil
}

// ValidateCSRFToken validates a CSRF token against a session token.
func (m *SessionManager) ValidateCSRFToken(sessionToken, csrfToken string) bool {
	expected, err := m.DeriveCSRFToken(sessionToken)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(csrfToken)))
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (m *SessionManager) sessionTTLFor(session *db.UserSession) time.Duration {
	ttl := m.ttl
	if session == nil {
		return ttl
	}

	if overrideTTL := m.extractTTLFromMetadata(session.Metadata); overrideTTL > 0 {
		ttl = overrideTTL
	}

	if session.Channel == "cli" && ttl < cliDefaultAbsoluteTTL {
		ttl = cliDefaultAbsoluteTTL
	}

	return ttl
}

func (m *SessionManager) extractTTLFromMetadata(metadata []byte) time.Duration {
	if len(metadata) == 0 {
		return 0
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return 0
	}

	raw, ok := meta["ttl_seconds"]
	if !ok {
		return 0
	}

	return m.parseTTLSeconds(raw)
}

func (m *SessionManager) parseTTLSeconds(raw interface{}) time.Duration {
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	}
	return 0
}
