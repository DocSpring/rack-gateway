package db

import (
	"encoding/json"
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

// LogTOTPAttempt records a TOTP verification attempt for rate limiting and audit
func (d *Database) LogTOTPAttempt(
	userID int64,
	methodID *int64,
	success bool,
	failureReason string,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) error {
	return d.logMFAAttempt(userID, methodID, MFAMethodTypeTOTP, success, failureReason, ipAddress, userAgent, sessionID)
}

// LogWebAuthnAttempt records a WebAuthn verification attempt for rate limiting and audit
func (d *Database) LogWebAuthnAttempt(
	userID int64,
	methodID *int64,
	success bool,
	failureReason string,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) error {
	return d.logMFAAttempt(
		userID,
		methodID,
		MFAMethodTypeWebAuthn,
		success,
		failureReason,
		ipAddress,
		userAgent,
		sessionID,
	)
}

// logMFAAttempt is the consolidated implementation for logging MFA attempts
func (d *Database) logMFAAttempt(
	userID int64,
	methodID *int64,
	methodType int,
	success bool,
	failureReason string,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) error {
	query := `
        INSERT INTO mfa_attempts
        (user_id, method_id, method_type, success, failure_reason, ip_address, user_agent, session_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `
	_, err := d.exec(
		query,
		userID,
		methodID,
		methodType,
		success,
		nullableString(failureReason, 255),
		nullableString(ipAddress, 45),
		nullableString(userAgent, 512),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to log MFA attempt: %w", err)
	}
	return nil
}

// CountRecentTOTPAttempts counts TOTP attempts in the last N minutes
func (d *Database) CountRecentTOTPAttempts(userID int64, minutes int) (int, error) {
	return d.countRecentMFAAttempts(userID, MFAMethodTypeTOTP, minutes)
}

// CountRecentWebAuthnAttempts counts WebAuthn attempts in the last N minutes
func (d *Database) CountRecentWebAuthnAttempts(userID int64, minutes int) (int, error) {
	return d.countRecentMFAAttempts(userID, MFAMethodTypeWebAuthn, minutes)
}

// countRecentMFAAttempts is the consolidated implementation for counting attempts
func (d *Database) countRecentMFAAttempts(userID int64, methodType int, minutes int) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) FROM mfa_attempts
        WHERE user_id = $1 AND method_type = $2 AND attempted_at > NOW() - INTERVAL '1 minute' * $3
    `
	err := d.queryRow(query, userID, methodType, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count MFA attempts: %w", err)
	}
	return count, nil
}

// CountRecentFailedMFAAttempts counts all failed MFA attempts (TOTP + WebAuthn) in the last N minutes.
func (d *Database) CountRecentFailedMFAAttempts(userID int64, minutes int) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) FROM mfa_attempts
        WHERE user_id = $1
          AND attempted_at > NOW() - INTERVAL '1 minute' * $2
          AND success = FALSE
    `
	err := d.queryRow(query, userID, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count failed MFA attempts: %w", err)
	}
	return count, nil
}

// LockUser locks a user account
func (d *Database) LockUser(userID int64, reason string, lockedByUserID *int64) error {
	_, err := d.exec(`
        UPDATE users
        SET locked_at = NOW(), locked_reason = ?, locked_by_user_id = ?, updated_at = NOW()
        WHERE id = ? AND locked_at IS NULL
    `, nullableString(reason, 255), lockedByUserID, userID)
	if err != nil {
		return fmt.Errorf("failed to lock user: %w", err)
	}
	return nil
}

// UnlockUser unlocks a user account by clearing the lock fields.
func (d *Database) UnlockUser(userID int64, _ int64) error {
	_, err := d.exec(`
        UPDATE users
        SET locked_at = NULL, locked_reason = NULL, locked_by_user_id = NULL, updated_at = NOW()
        WHERE id = ?
    `, userID)
	if err != nil {
		return fmt.Errorf("failed to unlock user: %w", err)
	}
	return nil
}

// IsUserLocked checks if a user account is currently locked
func (d *Database) IsUserLocked(userID int64) (bool, error) {
	var locked bool
	err := d.queryRow(`
        SELECT locked_at IS NOT NULL
        FROM users WHERE id = ?
    `, userID).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("failed to check if user is locked: %w", err)
	}
	return locked, nil
}
