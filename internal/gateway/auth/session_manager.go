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
)

const (
	sessionTokenByteLength    = 32
	defaultSessionIdleTimeout = 5 * time.Minute
	cliDefaultAbsoluteTTL     = 90 * 24 * time.Hour
)

type SessionManager struct {
	db     *db.Database
	secret []byte
	ttl    time.Duration
}

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

type SessionValidationResult struct {
	Session *db.UserSession
	User    *db.User
}

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

func (m *SessionManager) TTL() time.Duration {
	if m == nil {
		return 0
	}
	return m.ttl
}

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

func (m *SessionManager) ValidateSession(sessionToken, ipAddress, userAgent string) (*SessionValidationResult, error) {
	if m == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	trimmed := strings.TrimSpace(sessionToken)
	if trimmed == "" {
		return nil, fmt.Errorf("empty session token")
	}

	tokenHash := hashSessionToken(trimmed)
	session, user, err := m.db.GetUserSessionWithUserByHash(tokenHash)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("session not found")
	}
	if session.RevokedAt != nil {
		return nil, fmt.Errorf("session revoked")
	}
	now := time.Now()
	if session.ExpiresAt.Before(now) {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("session expired")
	}

	ttl := m.sessionTTLFor(session)
	if ttl > 0 && session.LastSeenAt.Add(ttl).Before(now) {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("session expired")
	}

	if user == nil {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("session user missing")
	}
	if user.Suspended {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("user suspended")
	}

	// Refresh idle timeout to enforce sliding expiration on activity.
	if err := m.db.TouchUserSession(session.ID, ipAddress, userAgent, now, now.Add(ttl)); err == nil {
		session.LastSeenAt = now
		session.ExpiresAt = now.Add(ttl)
		if trimmedIP := strings.TrimSpace(ipAddress); trimmedIP != "" {
			session.IPAddress = trimmedIP
		}
		if trimmedUA := strings.TrimSpace(userAgent); trimmedUA != "" {
			session.UserAgent = trimmedUA
		}
	}

	return &SessionValidationResult{Session: session, User: user}, nil
}

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

func (m *SessionManager) RevokeByID(id int64, revokedBy *int64) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("session manager not initialized")
	}
	if id <= 0 {
		return false, fmt.Errorf("invalid session id")
	}
	return m.db.RevokeUserSession(id, revokedBy)
}

func (m *SessionManager) RevokeAllForUser(userID int64, revokedBy *int64) (int64, error) {
	if m == nil {
		return 0, fmt.Errorf("session manager not initialized")
	}
	if userID <= 0 {
		return 0, fmt.Errorf("invalid user id")
	}
	return m.db.RevokeAllUserSessions(userID, revokedBy)
}

func (m *SessionManager) ListActiveForUser(userID int64) ([]*db.UserSession, error) {
	if m == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	return m.db.ListActiveSessionsByUser(userID)
}

func (m *SessionManager) UpdateSessionMFAVerified(sessionID int64, verifiedAt time.Time, trustedDeviceID *int64) error {
	if m == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return m.db.UpdateSessionMFAVerified(sessionID, verifiedAt, trustedDeviceID)
}

func (m *SessionManager) UpdateSessionRecentStepUp(sessionID int64, when time.Time) error {
	if m == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return m.db.UpdateSessionRecentStepUp(sessionID, when)
}

func (m *SessionManager) AttachTrustedDeviceToSession(sessionID int64, trustedDeviceID int64) error {
	if m == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return m.db.AttachTrustedDeviceToSession(sessionID, trustedDeviceID)
}

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

	if len(session.Metadata) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(session.Metadata, &meta); err == nil {
			if raw, ok := meta["ttl_seconds"]; ok {
				switch v := raw.(type) {
				case float64:
					if v > 0 {
						ttl = time.Duration(v * float64(time.Second))
					}
				case int64:
					if v > 0 {
						ttl = time.Duration(v) * time.Second
					}
				case int:
					if v > 0 {
						ttl = time.Duration(v) * time.Second
					}
				}
			}
		}
	}

	if session.Channel == "cli" && ttl < cliDefaultAbsoluteTTL {
		ttl = cliDefaultAbsoluteTTL
	}

	return ttl
}
