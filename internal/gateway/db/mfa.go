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

// SetUserMFAEnrolled updates the MFA enrollment status for a user.
// If enrolled is true, it also sets mfa_enforced_at if not already set.
func (d *Database) SetUserMFAEnrolled(userID int64, enrolled bool) error {
	query := `
		UPDATE users
		SET mfa_enrolled = ?,
		    mfa_enforced_at = CASE WHEN ? THEN COALESCE(mfa_enforced_at, NOW()) ELSE mfa_enforced_at END,
		    updated_at = NOW()
		WHERE id = ?
	`
	_, err := d.exec(query, enrolled, enrolled, userID)
	if err != nil {
		return fmt.Errorf("failed to update user MFA enrollment: %w", err)
	}
	return nil
}

// ResetUserMFA completely removes all MFA configuration for a user.
// This includes methods, backup codes, trusted devices, and active sessions.
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

	revokeDevicesQuery := `
		UPDATE trusted_devices
		SET revoked_at = NOW(), revoked_reason = 'reset', updated_at = NOW()
		WHERE user_id = ? AND revoked_at IS NULL
	`
	if _, err := d.execTx(tx, revokeDevicesQuery, userID); err != nil {
		return fmt.Errorf("failed to revoke trusted devices: %w", err)
	}

	revokeSessionsQuery := `
		UPDATE user_sessions
		SET revoked_at = NOW(), updated_at = NOW()
		WHERE user_id = ? AND revoked_at IS NULL
	`
	if _, err := d.execTx(tx, revokeSessionsQuery, userID); err != nil {
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

// CreateMFAMethod creates a new MFA method for a user.
// It supports both TOTP (with secret) and WebAuthn (with credentialID/publicKey).
func (d *Database) CreateMFAMethod(
	userID int64,
	methodType string,
	label string,
	secret string,
	credentialID []byte,
	publicKey []byte,
	transports []string,
	metadata map[string]interface{},
) (*MFAMethod, error) {
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

	row := d.queryRow(
		query,
		userID,
		methodType,
		nullableString(label, 150),
		secret,
		credentialID,
		publicKey,
		jsonStringArray(transports),
		meta,
	)
	if err := row.Scan(&id, &createdAt); err != nil {
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

// ConfirmMFAMethod marks an MFA method as confirmed and updates its last used time.
func (d *Database) ConfirmMFAMethod(methodID int64, confirmedAt time.Time) error {
	_, err := d.exec(
		"UPDATE mfa_methods SET confirmed_at = ?, last_used_at = ? WHERE id = ?",
		confirmedAt,
		confirmedAt,
		methodID,
	)
	if err != nil {
		return fmt.Errorf("failed to confirm MFA method: %w", err)
	}
	return nil
}

// DeleteUnconfirmedMFAMethods removes all unconfirmed MFA methods for a user.
func (d *Database) DeleteUnconfirmedMFAMethods(userID int64) error {
	_, err := d.exec("DELETE FROM mfa_methods WHERE user_id = ? AND confirmed_at IS NULL", userID)
	if err != nil {
		return fmt.Errorf("failed to delete unconfirmed MFA methods: %w", err)
	}
	return nil
}

// UpdateMFAMethodLastUsed updates the last used timestamp for an MFA method.
func (d *Database) UpdateMFAMethodLastUsed(methodID int64, lastUsedAt time.Time) error {
	_, err := d.exec("UPDATE mfa_methods SET last_used_at = ? WHERE id = ?", lastUsedAt, methodID)
	if err != nil {
		return fmt.Errorf("failed to update MFA method last used: %w", err)
	}
	return nil
}

// DeleteMFAMethod removes an MFA method by its ID.
func (d *Database) DeleteMFAMethod(methodID int64) error {
	_, err := d.exec("DELETE FROM mfa_methods WHERE id = ?", methodID)
	if err != nil {
		return fmt.Errorf("failed to delete MFA method: %w", err)
	}
	return nil
}

// ListMFAMethods returns all confirmed MFA methods for a user.
func (d *Database) ListMFAMethods(userID int64) ([]*MFAMethod, error) {
	return d.listMFAMethods(userID, false)
}

// ListAllMFAMethods returns all MFA methods for a user, including unconfirmed ones.
func (d *Database) ListAllMFAMethods(userID int64) ([]*MFAMethod, error) {
	return d.listMFAMethods(userID, true)
}

func (d *Database) listMFAMethods(userID int64, includeUnconfirmed bool) ([]*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata,
               created_at, confirmed_at, last_used_at
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

// UpdateMFAMethodLabel updates the label of an MFA method.
func (d *Database) UpdateMFAMethodLabel(methodID int64, label string) error {
	_, err := d.exec("UPDATE mfa_methods SET label = ? WHERE id = ?", nullableString(label, 150), methodID)
	if err != nil {
		return fmt.Errorf("failed to update mfa method label: %w", err)
	}
	return nil
}

// UpdateMFAMethodCredential updates a WebAuthn credential's details.
func (d *Database) UpdateMFAMethodCredential(
	methodID int64,
	methodType string,
	label string,
	credentialID []byte,
	publicKey []byte,
	transports []string,
	metadata []byte,
) error {
	query := `
		UPDATE mfa_methods
		SET type = ?, label = ?, credential_id = ?, public_key = ?, transports = ?, metadata = ?
		WHERE id = ?
	`
	_, err := d.exec(
		query,
		methodType,
		nullableString(label, 150),
		credentialID,
		publicKey,
		jsonStringArray(transports),
		string(metadata),
		methodID,
	)
	if err != nil {
		return fmt.Errorf("failed to update mfa method credential: %w", err)
	}
	return nil
}

// GetMFAMethodByID retrieves an MFA method by its ID.
func (d *Database) GetMFAMethodByID(id int64) (*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata,
               created_at, confirmed_at, last_used_at
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

// ReplaceBackupCodes replaces all backup codes for a user with a new set.
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

// ListBackupCodes returns all backup codes for a user.
func (d *Database) ListBackupCodes(userID int64) ([]*MFABackupCode, error) {
	query := `
		SELECT id, user_id, code_hash, created_at, used_at
		FROM mfa_backup_codes
		WHERE user_id = ?
		ORDER BY created_at
	`
	rows, err := d.query(query, userID)
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

// MarkBackupCodeUsed marks a backup code as used for a user.
// Returns true if a code was successfully marked, false if it was already used.
func (d *Database) MarkBackupCodeUsed(userID int64, hash string) (bool, error) {
	res, err := d.exec(
		"UPDATE mfa_backup_codes SET used_at = NOW() WHERE user_id = ? AND code_hash = ? AND used_at IS NULL",
		userID,
		hash,
	)
	if err != nil {
		return false, fmt.Errorf("failed to mark backup code used: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect backup code update: %w", err)
	}
	return n > 0, nil
}
