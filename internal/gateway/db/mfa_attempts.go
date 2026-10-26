package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func jsonStringArray(values []string) interface{} {
	if len(values) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return nil
	}
	return string(b)
}

// ConsumeTOTPTimeStep atomically marks a TOTP time-step as used.
// Returns true if successfully consumed (first use), false if already used (replay).
// This prevents replay attacks by ensuring each time-step can only be used once.
func (d *Database) ConsumeTOTPTimeStep(
	userID int64,
	timeStep int64,
	methodID *int64,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (bool, error) {
	query := `
        INSERT INTO used_totp_steps (user_id, time_step, method_id, ip_address, user_agent, session_id)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT (user_id, time_step) DO NOTHING
    `
	result, err := d.exec(
		query,
		userID,
		timeStep,
		methodID,
		nullableString(ipAddress, 45),
		nullableString(userAgent, 512),
		sessionID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to consume TOTP time-step: %w", err)
	}

	// Check if a row was actually inserted
	// If ON CONFLICT triggered, no row was inserted (rowsAffected = 0), meaning it was already used
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to check rows affected: %w", err)
	}

	// If rowsAffected > 0, the time-step was successfully consumed (first use)
	// If rowsAffected = 0, the time-step was already used (replay attack)
	return rowsAffected > 0, nil
}

// MFA method type constants used for logging and rate limiting.
const (
	MFAMethodTypeTOTP     = 1
	MFAMethodTypeWebAuthn = 2
)

var (

	// ErrMFAAttemptLimit is returned by ReserveMFAAttempt when the user has made the most failed attempts the window
	// allows.
	ErrMFAAttemptLimit = errors.New("too many failed MFA attempts")
	// ErrUserLocked is returned by ReserveMFAAttempt for a locked user.
	ErrUserLocked = errors.New("user is locked")
)

// MFAAttemptRequest describes an MFA verification attempt: whose, with which kind of factor, and from where.
type MFAAttemptRequest struct {
	UserID     int64
	MethodType int
	IPAddress  string
	UserAgent  string
	SessionID  *int64
}

// MFAAttempt is an MFA verification attempt recorded by ReserveMFAAttempt. It counts toward the rate limit at once,
// until Finish records that it succeeded, and toward the automatic lock (CountRecentFailedMFAAttempts) once Finish
// records that it failed.
type MFAAttempt struct {
	db *Database
	ID int64
}

// ReserveMFAAttempt records an attempt before its factor is checked. It holds the user's row lock while it checks
// that they aren't locked (ErrUserLocked) and have fewer than limit attempts of this kind in the last minutes that
// failed or are still being verified, so concurrent attempts are counted one at a time and can't pass the limit
// together. Successful attempts don't count: every command asks for a fresh MFA proof, and a person running several
// commands, or approving several deploys, verifies many times in a few minutes. Refused (rate_limited) attempts
// don't count either, or retries would keep the limit in force. Over the limit it records a rate_limited attempt
// and returns ErrMFAAttemptLimit.
func (d *Database) ReserveMFAAttempt(req MFAAttemptRequest, limit, minutes int) (*MFAAttempt, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin MFA attempt: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	err = tx.QueryRow(d.rebind("SELECT locked_at IS NOT NULL FROM users WHERE id = ? FOR UPDATE"),
		req.UserID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lock user for MFA attempt: %w", err)
	}
	if locked {
		return nil, ErrUserLocked
	}

	var count int
	err = tx.QueryRow(d.rebind(`SELECT COUNT(*) FROM mfa_attempts
		WHERE user_id = ? AND method_type = ? AND attempted_at > NOW() - INTERVAL '1 minute' * ?
		  AND success = FALSE AND COALESCE(failure_reason, '') <> 'rate_limited'`),
		req.UserID, req.MethodType, minutes).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to count MFA attempts: %w", err)
	}
	reason := "in_progress"
	if count >= limit {
		reason = "rate_limited"
	}
	var id int64
	err = tx.QueryRow(d.rebind(`INSERT INTO mfa_attempts
		(user_id, method_type, success, failure_reason, ip_address, user_agent, session_id)
		VALUES (?, ?, FALSE, ?, ?, ?, ?) RETURNING id`),
		req.UserID, req.MethodType, reason, nullableString(req.IPAddress, 45), nullableString(req.UserAgent, 512),
		req.SessionID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to log MFA attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit MFA attempt: %w", err)
	}
	if count >= limit {
		return nil, ErrMFAAttemptLimit
	}
	return &MFAAttempt{db: d, ID: id}, nil
}

// Discard deletes an attempt that couldn't have verified anything, so it doesn't count.
func (a *MFAAttempt) Discard() error {
	if _, err := a.db.exec("DELETE FROM mfa_attempts WHERE id = ?", a.ID); err != nil {
		return fmt.Errorf("failed to discard MFA attempt: %w", err)
	}
	return nil
}

// Finish records the attempt's outcome: the method it used (if any), whether it succeeded, and why not.
func (a *MFAAttempt) Finish(methodID *int64, success bool, failureReason string) error {
	_, err := a.db.exec("UPDATE mfa_attempts SET method_id = ?, success = ?, failure_reason = ? WHERE id = ?",
		methodID, success, nullableString(failureReason, 255), a.ID)
	if err != nil {
		return fmt.Errorf("failed to record MFA attempt outcome: %w", err)
	}
	return nil
}

// CountRecentFailedMFAAttempts counts the failed MFA verifications (TOTP + WebAuthn) in the last N minutes. An
// attempt still being verified (in_progress) hasn't failed, and a rate-limited one verified nothing.
func (d *Database) CountRecentFailedMFAAttempts(userID int64, minutes int) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) FROM mfa_attempts
        WHERE user_id = $1
          AND attempted_at > NOW() - INTERVAL '1 minute' * $2
          AND success = FALSE
          AND COALESCE(failure_reason, '') NOT IN ('in_progress', 'rate_limited')
    `
	err := d.queryRow(query, userID, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count failed MFA attempts: %w", err)
	}
	return count, nil
}
