package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata, created_at, confirmed_at, last_used_at
        FROM mfa_methods WHERE user_id = ? AND confirmed_at IS NOT NULL ORDER BY created_at
    `
	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list MFA methods: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var methods []*MFAMethod
	for rows.Next() {
		var method MFAMethod
		var label sql.NullString
		var secret sql.NullString
		var transports sql.NullString
		var metadata sql.NullString
		var confirmed sql.NullTime
		var lastUsed sql.NullTime
		if err := rows.Scan(&method.ID, &method.UserID, &method.Type, &label, &secret, &method.CredentialID, &method.PublicKey, &transports, &metadata, &method.CreatedAt, &confirmed, &lastUsed); err != nil {
			return nil, fmt.Errorf("failed to scan MFA method: %w", err)
		}
		if label.Valid {
			method.Label = label.String
		}
		if secret.Valid {
			method.Secret = secret.String
		}
		if transports.Valid {
			var arr []string
			if err := json.Unmarshal([]byte(transports.String), &arr); err == nil {
				method.Transports = arr
			}
		}
		if metadata.Valid {
			method.Metadata = []byte(metadata.String)
		}
		if confirmed.Valid {
			t := confirmed.Time
			method.ConfirmedAt = &t
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			method.LastUsedAt = &t
		}
		methods = append(methods, &method)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate MFA methods: %w", err)
	}
	return methods, nil
}

func (d *Database) ListAllMFAMethods(userID int64) ([]*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata, created_at, confirmed_at, last_used_at
        FROM mfa_methods WHERE user_id = ? ORDER BY created_at
    `
	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list all MFA methods: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var methods []*MFAMethod
	for rows.Next() {
		var method MFAMethod
		var label sql.NullString
		var secret sql.NullString
		var transports sql.NullString
		var metadata sql.NullString
		var confirmed sql.NullTime
		var lastUsed sql.NullTime
		if err := rows.Scan(&method.ID, &method.UserID, &method.Type, &label, &secret, &method.CredentialID, &method.PublicKey, &transports, &metadata, &method.CreatedAt, &confirmed, &lastUsed); err != nil {
			return nil, fmt.Errorf("failed to scan MFA method: %w", err)
		}
		if label.Valid {
			method.Label = label.String
		}
		if secret.Valid {
			method.Secret = secret.String
		}
		if transports.Valid {
			var arr []string
			if err := json.Unmarshal([]byte(transports.String), &arr); err == nil {
				method.Transports = arr
			}
		}
		if metadata.Valid {
			method.Metadata = []byte(metadata.String)
		}
		if confirmed.Valid {
			t := confirmed.Time
			method.ConfirmedAt = &t
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			method.LastUsedAt = &t
		}
		methods = append(methods, &method)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate all MFA methods: %w", err)
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
	var method MFAMethod
	var label sql.NullString
	var transports sql.NullString
	var metadata sql.NullString
	var confirmed sql.NullTime
	var lastUsed sql.NullTime
	err := d.queryRow(query, id).Scan(&method.ID, &method.UserID, &method.Type, &label, &method.Secret, &method.CredentialID, &method.PublicKey, &transports, &metadata, &method.CreatedAt, &confirmed, &lastUsed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get MFA method: %w", err)
	}
	if label.Valid {
		method.Label = label.String
	}
	if transports.Valid {
		var arr []string
		if err := json.Unmarshal([]byte(transports.String), &arr); err == nil {
			method.Transports = arr
		}
	}
	if metadata.Valid {
		method.Metadata = []byte(metadata.String)
	}
	if confirmed.Valid {
		t := confirmed.Time
		method.ConfirmedAt = &t
	}
	if lastUsed.Valid {
		t := lastUsed.Time
		method.LastUsedAt = &t
	}
	return &method, nil
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
	for _, hash := range codeHashes {
		if strings.TrimSpace(hash) == "" {
			continue
		}
		if _, err := d.execTx(tx, "INSERT INTO mfa_backup_codes (user_id, code_hash) VALUES (?, ?)", userID, hash); err != nil {
			return fmt.Errorf("failed to insert backup code: %w", err)
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

func (d *Database) CreateTrustedDevice(userID int64, deviceID string, tokenHash string, expiresAt time.Time, ip string, uaHash string, metadata map[string]interface{}) (*TrustedDevice, error) {
	var id int64
	var createdAt, updatedAt time.Time
	var meta interface{}
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			meta = string(b)
		}
	}
	query := `
        INSERT INTO trusted_devices (user_id, device_id, token_hash, expires_at, ip_first, ip_last, user_agent_hash, metadata)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING id, created_at, updated_at
    `
	if err := d.queryRow(query, userID, deviceID, tokenHash, expiresAt, nullableIP(ip), nullableIP(ip), nullableString(uaHash, 128), meta).Scan(&id, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("failed to create trusted device: %w", err)
	}
	return &TrustedDevice{
		ID:            id,
		UserID:        userID,
		DeviceID:      deviceID,
		TokenHash:     tokenHash,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ExpiresAt:     expiresAt,
		LastUsedAt:    createdAt,
		IPFirst:       strings.TrimSpace(ip),
		IPLast:        strings.TrimSpace(ip),
		UserAgentHash: strings.TrimSpace(uaHash),
	}, nil
}

func (d *Database) TouchTrustedDevice(id int64, ip string) error {
	_, err := d.exec("UPDATE trusted_devices SET last_used_at = NOW(), ip_last = COALESCE(?, ip_last), updated_at = NOW() WHERE id = ?", nullableIP(ip), id)
	if err != nil {
		return fmt.Errorf("failed to update trusted device usage: %w", err)
	}
	return nil
}

func (d *Database) GetTrustedDeviceByHash(hash string) (*TrustedDevice, error) {
	query := `
        SELECT id, user_id, device_id, token_hash, created_at, updated_at, expires_at, last_used_at, ip_first, ip_last, user_agent_hash, revoked_at, revoked_reason, metadata
        FROM trusted_devices WHERE token_hash = ?
    `
	var device TrustedDevice
	var ipFirst sql.NullString
	var ipLast sql.NullString
	var ua sql.NullString
	var revoked sql.NullTime
	var reason sql.NullString
	var metadata sql.NullString
	err := d.queryRow(query, hash).Scan(&device.ID, &device.UserID, &device.DeviceID, &device.TokenHash, &device.CreatedAt, &device.UpdatedAt, &device.ExpiresAt, &device.LastUsedAt, &ipFirst, &ipLast, &ua, &revoked, &reason, &metadata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get trusted device: %w", err)
	}
	if ipFirst.Valid {
		device.IPFirst = ipFirst.String
	}
	if ipLast.Valid {
		device.IPLast = ipLast.String
	}
	if ua.Valid {
		device.UserAgentHash = ua.String
	}
	if revoked.Valid {
		t := revoked.Time
		device.RevokedAt = &t
	}
	if reason.Valid {
		device.RevokedReason = reason.String
	}
	if metadata.Valid {
		device.Metadata = json.RawMessage(metadata.String)
	}
	return &device, nil
}

func (d *Database) GetTrustedDeviceByID(id int64) (*TrustedDevice, error) {
	query := `
        SELECT id, user_id, device_id, token_hash, created_at, updated_at, expires_at, last_used_at, ip_first, ip_last, user_agent_hash, revoked_at, revoked_reason, metadata
        FROM trusted_devices WHERE id = ?
    `
	var device TrustedDevice
	var ipFirst sql.NullString
	var ipLast sql.NullString
	var ua sql.NullString
	var revoked sql.NullTime
	var reason sql.NullString
	var metadata sql.NullString
	err := d.queryRow(query, id).Scan(&device.ID, &device.UserID, &device.DeviceID, &device.TokenHash, &device.CreatedAt, &device.UpdatedAt, &device.ExpiresAt, &device.LastUsedAt, &ipFirst, &ipLast, &ua, &revoked, &reason, &metadata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get trusted device: %w", err)
	}
	if ipFirst.Valid {
		device.IPFirst = ipFirst.String
	}
	if ipLast.Valid {
		device.IPLast = ipLast.String
	}
	if ua.Valid {
		device.UserAgentHash = ua.String
	}
	if revoked.Valid {
		t := revoked.Time
		device.RevokedAt = &t
	}
	if reason.Valid {
		device.RevokedReason = reason.String
	}
	if metadata.Valid {
		device.Metadata = json.RawMessage(metadata.String)
	}
	return &device, nil
}

func (d *Database) RevokeTrustedDevice(id int64, reason string) error {
	_, err := d.exec("UPDATE trusted_devices SET revoked_at = NOW(), revoked_reason = ?, updated_at = NOW() WHERE id = ? AND revoked_at IS NULL", nullableString(reason, 255), id)
	if err != nil {
		return fmt.Errorf("failed to revoke trusted device: %w", err)
	}
	return nil
}

func (d *Database) ListTrustedDevices(userID int64) ([]*TrustedDevice, error) {
	rows, err := d.query(`SELECT id, user_id, device_id, token_hash, created_at, updated_at, expires_at, last_used_at, ip_first, ip_last, user_agent_hash, revoked_at, revoked_reason, metadata FROM trusted_devices WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query trusted devices: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var devices []*TrustedDevice
	for rows.Next() {
		var device TrustedDevice
		var ipFirst sql.NullString
		var ipLast sql.NullString
		var ua sql.NullString
		var revoked sql.NullTime
		var reason sql.NullString
		var metadata sql.NullString
		if err := rows.Scan(&device.ID, &device.UserID, &device.DeviceID, &device.TokenHash, &device.CreatedAt, &device.UpdatedAt, &device.ExpiresAt, &device.LastUsedAt, &ipFirst, &ipLast, &ua, &revoked, &reason, &metadata); err != nil {
			return nil, fmt.Errorf("failed to scan trusted device: %w", err)
		}
		if ipFirst.Valid {
			device.IPFirst = ipFirst.String
		}
		if ipLast.Valid {
			device.IPLast = ipLast.String
		}
		if ua.Valid {
			device.UserAgentHash = ua.String
		}
		if revoked.Valid {
			t := revoked.Time
			device.RevokedAt = &t
		}
		if reason.Valid {
			device.RevokedReason = reason.String
		}
		if metadata.Valid {
			device.Metadata = json.RawMessage(metadata.String)
		}
		devices = append(devices, &device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate trusted devices: %w", err)
	}
	return devices, nil
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

// LogTOTPAttempt records a TOTP verification attempt for replay protection, rate limiting, and audit
func (d *Database) LogTOTPAttempt(userID int64, methodID *int64, codeHash string, success bool, failureReason string, ipAddress string, userAgent string, sessionID *int64) error {
	_, err := d.exec(`
        INSERT INTO mfa_totp_attempts (user_id, method_id, code_hash, success, failure_reason, ip_address, user_agent, session_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, userID, methodID, codeHash, success, nullableString(failureReason, 255), nullableString(ipAddress, 45), nullableString(userAgent, 512), sessionID)
	if err != nil {
		return fmt.Errorf("failed to log TOTP attempt: %w", err)
	}
	return nil
}

// CountRecentTOTPAttempts counts TOTP attempts in the last N minutes
func (d *Database) CountRecentTOTPAttempts(userID int64, minutes int) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) FROM mfa_totp_attempts
        WHERE user_id = $1 AND attempted_at > NOW() - INTERVAL '1 minute' * $2
    `
	err := d.queryRow(query, userID, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count TOTP attempts: %w", err)
	}
	return count, nil
}

// CheckTOTPReplay checks if a code was successfully used in the last N minutes
func (d *Database) CheckTOTPReplay(userID int64, codeHash string, minutes int) (bool, error) {
	var exists bool
	query := `
        SELECT EXISTS(
            SELECT 1 FROM mfa_totp_attempts
            WHERE user_id = $1 AND code_hash = $2 AND attempted_at > NOW() - INTERVAL '1 minute' * $3 AND success = TRUE
        )
    `
	err := d.queryRow(query, userID, codeHash, minutes).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check TOTP replay: %w", err)
	}
	return exists, nil
}

// LogWebAuthnAttempt records a WebAuthn verification attempt for rate limiting and audit
func (d *Database) LogWebAuthnAttempt(userID int64, methodID *int64, success bool, failureReason string, ipAddress string, userAgent string, sessionID *int64) error {
	_, err := d.exec(`
        INSERT INTO mfa_webauthn_attempts (user_id, method_id, success, failure_reason, ip_address, user_agent, session_id)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, userID, methodID, success, nullableString(failureReason, 255), nullableString(ipAddress, 45), nullableString(userAgent, 512), sessionID)
	if err != nil {
		return fmt.Errorf("failed to log WebAuthn attempt: %w", err)
	}
	return nil
}

// CountRecentWebAuthnAttempts counts WebAuthn attempts in the last N minutes
func (d *Database) CountRecentWebAuthnAttempts(userID int64, minutes int) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) FROM mfa_webauthn_attempts
        WHERE user_id = $1 AND attempted_at > NOW() - INTERVAL '1 minute' * $2
    `
	err := d.queryRow(query, userID, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count WebAuthn attempts: %w", err)
	}
	return count, nil
}

// CountRecentFailedMFAAttempts counts all failed MFA attempts (TOTP + WebAuthn) in the last N minutes
func (d *Database) CountRecentFailedMFAAttempts(userID int64, minutes int) (int, error) {
	var count int
	query := `
        SELECT
            (SELECT COUNT(*) FROM mfa_totp_attempts WHERE user_id = $1 AND attempted_at > NOW() - INTERVAL '1 minute' * $2 AND success = FALSE) +
            (SELECT COUNT(*) FROM mfa_webauthn_attempts WHERE user_id = $3 AND attempted_at > NOW() - INTERVAL '1 minute' * $4 AND success = FALSE)
    `
	err := d.queryRow(query, userID, minutes, userID, minutes).Scan(&count)
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

// UnlockUser unlocks a user account
func (d *Database) UnlockUser(userID int64, unlockedByUserID int64) error {
	_, err := d.exec(`
        UPDATE users
        SET unlocked_at = NOW(), unlocked_by_user_id = ?, updated_at = NOW()
        WHERE id = ?
    `, unlockedByUserID, userID)
	if err != nil {
		return fmt.Errorf("failed to unlock user: %w", err)
	}
	return nil
}

// IsUserLocked checks if a user account is currently locked
func (d *Database) IsUserLocked(userID int64) (bool, error) {
	var locked bool
	err := d.queryRow(`
        SELECT locked_at IS NOT NULL AND (unlocked_at IS NULL OR unlocked_at < locked_at)
        FROM users WHERE id = ?
    `, userID).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("failed to check if user is locked: %w", err)
	}
	return locked, nil
}
