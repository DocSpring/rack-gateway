package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// WebAuthn challenge purposes.
const (
	WebAuthnChallengeAssertion    = "assertion"
	WebAuthnChallengeRegistration = "registration"
)

// ErrWebAuthnChallengeNotFound is returned when a challenge is unknown, expired, already used,
// or belongs to a different user or session.
var ErrWebAuthnChallengeNotFound = errors.New("webauthn challenge not found or expired")

// WebAuthnChallenge is a server-side WebAuthn ceremony (go-webauthn session data).
type WebAuthnChallenge struct {
	ID          string
	UserID      int64
	SessionID   *int64
	Purpose     string
	SessionData []byte
}

// CreateWebAuthnChallenge stores a challenge and returns its opaque ID. Expired challenges are purged
// opportunistically. Only one registration challenge is kept per user and session.
func (d *Database) CreateWebAuthnChallenge(
	userID int64,
	sessionID *int64,
	purpose string,
	sessionData []byte,
	ttl time.Duration,
) (string, error) {
	id, err := newChallengeID()
	if err != nil {
		return "", err
	}
	if _, err := d.exec("DELETE FROM webauthn_challenges WHERE expires_at <= NOW()"); err != nil {
		return "", fmt.Errorf("failed to purge expired webauthn challenges: %w", err)
	}
	if purpose == WebAuthnChallengeRegistration {
		if _, err := d.exec(`
			DELETE FROM webauthn_challenges
			WHERE user_id = ? AND session_id IS NOT DISTINCT FROM ?::BIGINT AND purpose = ?`,
			userID, nullableInt64(sessionID), purpose,
		); err != nil {
			return "", fmt.Errorf("failed to replace webauthn registration challenge: %w", err)
		}
	}
	_, err = d.exec(`
		INSERT INTO webauthn_challenges (id, user_id, session_id, purpose, session_data, expires_at)
		VALUES (?, ?, ?, ?, ?, NOW() + make_interval(secs => ?))`,
		id, userID, nullableInt64(sessionID), purpose, string(sessionData), ttl.Seconds(),
	)
	if err != nil {
		return "", fmt.Errorf("failed to store webauthn challenge: %w", err)
	}
	return id, nil
}

// ConsumeWebAuthnChallenge atomically deletes and returns an unexpired challenge owned by userID.
// When the challenge was started from a session and sessionID is given, the sessions must match.
func (d *Database) ConsumeWebAuthnChallenge(
	id string,
	userID int64,
	sessionID *int64,
	purpose string,
) (*WebAuthnChallenge, error) {
	row := d.queryRow(`
		DELETE FROM webauthn_challenges
		WHERE id = ? AND user_id = ? AND purpose = ? AND expires_at > NOW()
		  AND (session_id IS NULL OR ?::BIGINT IS NULL OR session_id = ?::BIGINT)
		RETURNING id, user_id, session_id, purpose, session_data`,
		id, userID, purpose, nullableInt64(sessionID), nullableInt64(sessionID),
	)
	return scanWebAuthnChallenge(row)
}

// ConsumeSessionWebAuthnChallenge atomically deletes and returns the unexpired challenge of the given
// purpose for a user's session (used for registration, where the client holds no challenge ID).
func (d *Database) ConsumeSessionWebAuthnChallenge(
	userID int64,
	sessionID int64,
	purpose string,
) (*WebAuthnChallenge, error) {
	row := d.queryRow(`
		DELETE FROM webauthn_challenges
		WHERE id = (
			SELECT id FROM webauthn_challenges
			WHERE user_id = ? AND session_id = ? AND purpose = ? AND expires_at > NOW()
			ORDER BY created_at DESC LIMIT 1
		)
		RETURNING id, user_id, session_id, purpose, session_data`,
		userID, sessionID, purpose,
	)
	return scanWebAuthnChallenge(row)
}

// UpdateMFAMethodSignCount stores the latest WebAuthn signature counter for a credential.
func (d *Database) UpdateMFAMethodSignCount(methodID int64, signCount uint32) error {
	_, err := d.exec("UPDATE mfa_methods SET webauthn_sign_count = ? WHERE id = ?", signCount, methodID)
	if err != nil {
		return fmt.Errorf("failed to update webauthn sign count: %w", err)
	}
	return nil
}

func scanWebAuthnChallenge(row *sql.Row) (*WebAuthnChallenge, error) {
	var challenge WebAuthnChallenge
	var sessionID sql.NullInt64
	var data string
	err := row.Scan(&challenge.ID, &challenge.UserID, &sessionID, &challenge.Purpose, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWebAuthnChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume webauthn challenge: %w", err)
	}
	if sessionID.Valid {
		v := sessionID.Int64
		challenge.SessionID = &v
	}
	challenge.SessionData = []byte(data)
	return &challenge, nil
}

func newChallengeID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate webauthn challenge id: %w", err)
	}
	return "wac_" + base64.RawURLEncoding.EncodeToString(buf), nil
}
