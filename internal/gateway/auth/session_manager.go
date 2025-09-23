package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
)

const (
	sessionTokenByteLength   = 32
	sessionActivityUpdateTTL = 5 * time.Minute
)

type SessionManager struct {
	db     *db.Database
	secret []byte
	ttl    time.Duration
}

type SessionMetadata struct {
	IPAddress string
	UserAgent string
	Extra     map[string]interface{}
}

type SessionValidationResult struct {
	Session *db.UserSession
	User    *db.User
}

func NewSessionManager(database *db.Database, secret string, ttl time.Duration) *SessionManager {
	sm := &SessionManager{
		db:     database,
		secret: []byte(secret),
		ttl:    ttl,
	}
	if sm.ttl <= 0 {
		sm.ttl = 30 * 24 * time.Hour
	}
	return sm
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

	expiresAt := time.Now().Add(m.ttl)

	session, err := m.db.CreateUserSession(user.ID, tokenHash, expiresAt, meta.IPAddress, meta.UserAgent, meta.Extra)
	if err != nil {
		return "", nil, err
	}

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
	session, err := m.db.GetUserSessionByHash(tokenHash)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("session not found")
	}
	if session.RevokedAt != nil {
		return nil, fmt.Errorf("session revoked")
	}
	if time.Now().After(session.ExpiresAt) {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("session expired")
	}

	user, err := m.db.GetUserByID(session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("session user missing")
	}
	if user.Suspended {
		_, _ = m.db.RevokeUserSession(session.ID, nil)
		return nil, fmt.Errorf("user suspended")
	}

	if time.Since(session.LastSeenAt) > sessionActivityUpdateTTL {
		_ = m.db.TouchUserSession(session.ID, ipAddress, userAgent, time.Now())
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
