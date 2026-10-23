package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
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

// SessionAwaitingMFA returns true when the session belongs to a user who must
// complete an MFA challenge but has not yet done so for this session.
func SessionAwaitingMFA(settings *MFASettings, user *User, session *UserSession) bool {
	if session == nil || session.MFAVerifiedAt != nil {
		return false
	}
	return IsMFAChallengeRequired(settings, user)
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

// ResetUserMFA removes a user's MFA for an admin (resetBy): their methods, backup codes and trusted devices, and
// it revokes their sessions and deletes their CLI logins in progress (which may have passed MFA already), all in
// one transaction. Their API tokens are kept. They enroll a new method when they next sign in.
//
// It takes the user's row lock first, like every other change to a user's MFA, so it can't deadlock with them, and
// a login can't add a session it misses: a login checks the user's MFA enrollment under the same lock
// (CreateUserSession), so it either finishes before the reset, whose revocation then covers its session, or finds
// the user no longer enrolled.
func (d *Database) ResetUserMFA(userID, resetBy int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin MFA reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.lockUserTx(tx, userID); err != nil {
		return err
	}
	if _, err := d.execTx(tx, "DELETE FROM mfa_methods WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to delete mfa methods: %w", err)
	}
	if _, err := d.execTx(tx, "DELETE FROM mfa_backup_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to delete backup codes: %w", err)
	}

	if err := d.revokeTrustedDevicesTx(tx, userID, "admin_mfa_reset"); err != nil {
		return err
	}

	revokeSessionsQuery := `
		UPDATE user_sessions
		SET revoked_at = NOW(), revoked_by_user_id = ?, updated_at = NOW()
		WHERE user_id = ? AND revoked_at IS NULL
	`
	if _, err := d.execTx(tx, revokeSessionsQuery, resetBy, userID); err != nil {
		return fmt.Errorf("failed to revoke active sessions: %w", err)
	}

	pendingCLILogins := `
		DELETE FROM cli_login_states
		WHERE lower(login_email) = (SELECT lower(email) FROM users WHERE id = ?)
	`
	if _, err := d.execTx(tx, pendingCLILogins, userID); err != nil {
		return fmt.Errorf("failed to delete pending CLI logins: %w", err)
	}

	clearEnrolled := "UPDATE users SET mfa_enrolled = FALSE, updated_at = NOW() WHERE id = ?"
	if _, err := d.execTx(tx, clearEnrolled, userID); err != nil {
		return fmt.Errorf("failed to update user MFA flag: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit MFA reset: %w", err)
	}
	return nil
}

// CreateMFAMethod creates a new MFA method for a user.
// It supports both TOTP (with secret) and WebAuthn (with credentialID/publicKey).
// The secret is stored encrypted and bound to the user (see column_encryption.go).
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

	// Methods without a secret (WebAuthn) store NULL.
	var encryptedSecret sql.NullString
	if secret != "" {
		value, err := d.cipher.Encrypt(secret, colcrypt.MFASecretScope(userID, methodType))
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt MFA secret: %w", err)
		}
		encryptedSecret = sql.NullString{String: value, Valid: true}
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
		encryptedSecret,
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

// ConfirmMFAMethod marks an MFA method as confirmed and updates its last used time. It returns
// ErrMFAMethodNotFound when the method no longer exists.
func (d *Database) ConfirmMFAMethod(methodID int64, confirmedAt time.Time) error {
	res, err := d.exec(
		"UPDATE mfa_methods SET confirmed_at = ?, last_used_at = ? WHERE id = ?",
		confirmedAt,
		confirmedAt,
		methodID,
	)
	if err != nil {
		return fmt.Errorf("failed to confirm MFA method: %w", err)
	}
	return requireAffectedRow(res, ErrMFAMethodNotFound)
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

// ListMFAMethods returns all confirmed MFA methods for a user.
func (d *Database) ListMFAMethods(userID int64) ([]*MFAMethod, error) {
	return d.queryMFAMethods(userID, false)
}

// ListAllMFAMethods returns all MFA methods for a user, including unconfirmed ones.
func (d *Database) ListAllMFAMethods(userID int64) ([]*MFAMethod, error) {
	return d.queryMFAMethods(userID, true)
}

func (d *Database) queryMFAMethods(userID int64, includeUnconfirmed bool) ([]*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata,
               cli_capable, webauthn_sign_count, created_at, confirmed_at, last_used_at
        FROM mfa_methods WHERE user_id = ?`
	if !includeUnconfirmed {
		query += " AND confirmed_at IS NOT NULL"
	}
	query += " ORDER BY created_at"

	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list MFA methods: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var methods []*MFAMethod
	for rows.Next() {
		method, err := d.scanMFAMethod(rows)
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

// UpdateMFAMethodCLICapable updates whether an MFA method can be used for CLI authentication.
// This is only relevant for WebAuthn methods (browser-only vs hardware keys).
func (d *Database) UpdateMFAMethodCLICapable(methodID int64, cliCapable bool) error {
	_, err := d.exec("UPDATE mfa_methods SET cli_capable = ? WHERE id = ?", cliCapable, methodID)
	if err != nil {
		return fmt.Errorf("failed to update mfa method cli_capable: %w", err)
	}
	return nil
}

// GetMFAMethodByID retrieves an MFA method by its ID.
func (d *Database) GetMFAMethodByID(id int64) (*MFAMethod, error) {
	query := `
        SELECT id, user_id, type, label, secret, credential_id, public_key, transports, metadata,
               cli_capable, webauthn_sign_count, created_at, confirmed_at, last_used_at
        FROM mfa_methods WHERE id = ?
    `
	method, err := d.scanMFAMethod(d.queryRow(query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get MFA method: %w", err)
	}
	return method, nil
}

// lockUserTx takes the user's row lock for the rest of tx, or returns ErrUserNotFound. Changes to a user's MFA,
// sessions and roles take it before they touch anything else, so they can't deadlock with each other.
func (d *Database) lockUserTx(tx *sql.Tx, userID int64) error {
	var locked int64
	err := tx.QueryRow(d.rebind("SELECT id FROM users WHERE id = ? FOR UPDATE"), userID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to lock user %d: %w", userID, err)
	}
	return nil
}
