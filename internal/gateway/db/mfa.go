package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ShouldEnforceMFA returns true when the user is subject to MFA enforcement
// (e.g. require_all_users policy or a per-user enforcement flag).
func ShouldEnforceMFA(settings *MFASettings, user *User) bool {
	if user == nil {
		if settings == nil {
			return true
		}
		return settings.RequireAllUsers
	}
	if settings == nil {
		return true
	}
	if settings.RequireAllUsers {
		return true
	}
	return user.MFAEnforcedAt != nil
}

// IsMFAChallengeRequired returns true when the user must complete an MFA
// challenge to proceed (i.e. enforcement is active and the user is enrolled).
func IsMFAChallengeRequired(settings *MFASettings, user *User) bool {
	if user == nil {
		return false
	}
	if !user.MFAEnrolled {
		return false
	}
	return ShouldEnforceMFA(settings, user)
}

func (d *Database) SetUserMFAEnrolled(userID int64, enrolled bool) error {
	_, err := d.exec("UPDATE users SET mfa_enrolled = ?, mfa_enforced_at = CASE WHEN ? THEN COALESCE(mfa_enforced_at, NOW()) ELSE mfa_enforced_at END, updated_at = NOW() WHERE id = ?", enrolled, enrolled, userID)
	if err != nil {
		return fmt.Errorf("failed to update user MFA enrollment: %w", err)
	}
	return nil
}

func (d *Database) ResetUserMFA(userID int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin MFA reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := d.execTx(tx, "DELETE FROM mfa_methods WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to delete mfa methods: %w", err)
	}
	if _, err := d.execTx(tx, "DELETE FROM mfa_backup_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to delete backup codes: %w", err)
	}
	if _, err := d.execTx(tx, "UPDATE trusted_devices SET revoked_at = NOW(), revoked_reason = 'reset', updated_at = NOW() WHERE user_id = ? AND revoked_at IS NULL", userID); err != nil {
		return fmt.Errorf("failed to revoke trusted devices: %w", err)
	}
	if _, err := d.execTx(tx, "UPDATE user_sessions SET revoked_at = NOW(), updated_at = NOW() WHERE user_id = ? AND revoked_at IS NULL", userID); err != nil {
		return fmt.Errorf("failed to revoke active sessions: %w", err)
	}
	if _, err := d.execTx(tx, "UPDATE users SET mfa_enrolled = FALSE WHERE id = ?", userID); err != nil {
		return fmt.Errorf("failed to update user MFA flag: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit MFA reset: %w", err)
	}
	return nil
}

func (d *Database) CreateMFAMethod(userID int64, methodType string, label string, secret string, credentialID []byte, publicKey []byte, transports []string, metadata map[string]interface{}) (*MFAMethod, error) {
	var meta interface{}
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal mfa metadata: %w", err)
		}
		meta = string(b)
	}

	var id int64
	var createdAt time.Time
	query := `
        INSERT INTO mfa_methods (user_id, type, label, secret, credential_id, public_key, transports, metadata)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING id, created_at
    `

	if err := d.queryRow(query, userID, methodType, nullableString(label, 150), secret, credentialID, publicKey, jsonStringArray(transports), meta).Scan(&id, &createdAt); err != nil {
		return nil, fmt.Errorf("failed to create MFA method: %w", err)
	}

	return &MFAMethod{
		ID:        id,
		UserID:    userID,
		Type:      methodType,
		Label:     strings.TrimSpace(label),
		Secret:    secret,
		CreatedAt: createdAt,
	}, nil
}

func (d *Database) ConfirmMFAMethod(methodID int64, confirmedAt time.Time) error {
	_, err := d.exec("UPDATE mfa_methods SET confirmed_at = ?, last_used_at = ? WHERE id = ?", confirmedAt, confirmedAt, methodID)
	if err != nil {
		return fmt.Errorf("failed to confirm MFA method: %w", err)
	}
	return nil
}

func (d *Database) DeleteUnconfirmedMFAMethods(userID int64) error {
	_, err := d.exec("DELETE FROM mfa_methods WHERE user_id = ? AND confirmed_at IS NULL", userID)
	if err != nil {
		return fmt.Errorf("failed to delete unconfirmed MFA methods: %w", err)
	}
	return nil
}

func (d *Database) UpdateMFAMethodLastUsed(methodID int64, lastUsedAt time.Time) error {
	_, err := d.exec("UPDATE mfa_methods SET last_used_at = ? WHERE id = ?", lastUsedAt, methodID)
	if err != nil {
		return fmt.Errorf("failed to update MFA method last used: %w", err)
	}
	return nil
}

func (d *Database) DeleteMFAMethod(methodID int64) error {
	_, err := d.exec("DELETE FROM mfa_methods WHERE id = ?", methodID)
	if err != nil {
		return fmt.Errorf("failed to delete MFA method: %w", err)
	}
	return nil
}

func (d *Database) ListMFAMethods(userID int64) ([]*MFAMethod, error) {
	return d.listMFAMethods(userID, false)
}

func (d *Database) ListAllMFAMethods(userID int64) ([]*MFAMethod, error) {
	return d.listMFAMethods(userID, true)
}

func (d *Database) listMFAMethods(userID int64, includeUnconfirmed bool) ([]*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata, created_at, confirmed_at, last_used_at
        FROM mfa_methods WHERE user_id = ?`
	if !includeUnconfirmed {
		query += " AND confirmed_at IS NOT NULL"
	}
	query += " ORDER BY created_at"

	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list MFA methods: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var methods []*MFAMethod
	for rows.Next() {
		method, err := scanMFAMethod(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan MFA method: %w", err)
		}
		methods = append(methods, method)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate MFA methods: %w", err)
	}
	return methods, nil
}

func (d *Database) UpdateMFAMethodLabel(methodID int64, label string) error {
	_, err := d.exec("UPDATE mfa_methods SET label = ? WHERE id = ?", nullableString(label, 150), methodID)
	if err != nil {
		return fmt.Errorf("failed to update mfa method label: %w", err)
	}
	return nil
}

func (d *Database) UpdateMFAMethodCredential(methodID int64, methodType string, label string, credentialID []byte, publicKey []byte, transports []string, metadata []byte) error {
	query := `
		UPDATE mfa_methods
		SET type = ?, label = ?, credential_id = ?, public_key = ?, transports = ?, metadata = ?
		WHERE id = ?
	`
	_, err := d.exec(query, methodType, nullableString(label, 150), credentialID, publicKey, jsonStringArray(transports), string(metadata), methodID)
	if err != nil {
		return fmt.Errorf("failed to update mfa method credential: %w", err)
	}
	return nil
}

func (d *Database) GetMFAMethodByID(id int64) (*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata, created_at, confirmed_at, last_used_at
        FROM mfa_methods WHERE id = ?
    `
	method, err := scanMFAMethod(d.queryRow(query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get MFA method: %w", err)
	}
	return method, nil
}

func (d *Database) ReplaceBackupCodes(userID int64, codeHashes []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction for backup codes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := d.execTx(tx, "DELETE FROM mfa_backup_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to clear backup codes: %w", err)
	}

	// Filter empty hashes
	validHashes := make([]string, 0, len(codeHashes))
	for _, hash := range codeHashes {
		if strings.TrimSpace(hash) != "" {
			validHashes = append(validHashes, hash)
		}
	}

	// Bulk insert all backup codes in a single query
	if len(validHashes) > 0 {
		placeholders := make([]string, len(validHashes))
		args := make([]interface{}, 0, len(validHashes)*2)
		for i, hash := range validHashes {
			placeholders[i] = "(?, ?)"
			args = append(args, userID, hash)
		}
		query := "INSERT INTO mfa_backup_codes (user_id, code_hash) VALUES " + strings.Join(placeholders, ", ")
		if _, err := d.execTx(tx, query, args...); err != nil {
			return fmt.Errorf("failed to insert backup codes: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit backup code replacement: %w", err)
	}
	return nil
}

func (d *Database) ListBackupCodes(userID int64) ([]*MFABackupCode, error) {
	rows, err := d.query("SELECT id, user_id, code_hash, created_at, used_at FROM mfa_backup_codes WHERE user_id = ? ORDER BY created_at", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup codes: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var codes []*MFABackupCode
	for rows.Next() {
		var code MFABackupCode
		var used sql.NullTime
		if err := rows.Scan(&code.ID, &code.UserID, &code.CodeHash, &code.CreatedAt, &used); err != nil {
			return nil, fmt.Errorf("failed to scan backup code: %w", err)
		}
		if used.Valid {
			t := used.Time
			code.UsedAt = &t
		}
		codes = append(codes, &code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate backup codes: %w", err)
	}
	return codes, nil
}

func (d *Database) MarkBackupCodeUsed(userID int64, hash string) (bool, error) {
	res, err := d.exec("UPDATE mfa_backup_codes SET used_at = NOW() WHERE user_id = ? AND code_hash = ? AND used_at IS NULL", userID, hash)
	if err != nil {
		return false, fmt.Errorf("failed to mark backup code used: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect backup code update: %w", err)
	}
	return n > 0, nil
}

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
func (d *Database) ConsumeTOTPTimeStep(userID int64, timeStep int64, methodID *int64, ipAddress string, userAgent string, sessionID *int64) (bool, error) {
	result, err := d.exec(`
        INSERT INTO used_totp_steps (user_id, time_step, method_id, ip_address, user_agent, session_id)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT (user_id, time_step) DO NOTHING
    `, userID, timeStep, methodID, nullableString(ipAddress, 45), nullableString(userAgent, 512), sessionID)

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

const (
	MFAMethodTypeTOTP     = 1
	MFAMethodTypeWebAuthn = 2
)

// LogTOTPAttempt records a TOTP verification attempt for rate limiting and audit
func (d *Database) LogTOTPAttempt(userID int64, methodID *int64, success bool, failureReason string, ipAddress string, userAgent string, sessionID *int64) error {
	return d.logMFAAttempt(userID, methodID, MFAMethodTypeTOTP, success, failureReason, ipAddress, userAgent, sessionID)
}

// LogWebAuthnAttempt records a WebAuthn verification attempt for rate limiting and audit
func (d *Database) LogWebAuthnAttempt(userID int64, methodID *int64, success bool, failureReason string, ipAddress string, userAgent string, sessionID *int64) error {
	return d.logMFAAttempt(userID, methodID, MFAMethodTypeWebAuthn, success, failureReason, ipAddress, userAgent, sessionID)
}

// logMFAAttempt is the consolidated implementation for logging MFA attempts
func (d *Database) logMFAAttempt(userID int64, methodID *int64, methodType int, success bool, failureReason string, ipAddress string, userAgent string, sessionID *int64) error {
	_, err := d.exec(`
        INSERT INTO mfa_attempts (user_id, method_id, method_type, success, failure_reason, ip_address, user_agent, session_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, userID, methodID, methodType, success, nullableString(failureReason, 255), nullableString(ipAddress, 45), nullableString(userAgent, 512), sessionID)
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

// CountRecentFailedMFAAttempts counts all failed MFA attempts (TOTP + WebAuthn) in the last N minutes
func (d *Database) CountRecentFailedMFAAttempts(userID int64, minutes int) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) FROM mfa_attempts
        WHERE user_id = $1 AND attempted_at > NOW() - INTERVAL '1 minute' * $2 AND success = FALSE
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

// UnlockUser unlocks a user account by clearing the lock fields
func (d *Database) UnlockUser(userID int64, unlockedByUserID int64) error {
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
