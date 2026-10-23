package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrMFAMethodNotFound is returned when the MFA method to confirm no longer exists, for example because a new
	// enrollment deleted the user's unconfirmed methods.
	ErrMFAMethodNotFound = errors.New("MFA method not found")
	// ErrMFAEnrolledDuringSignIn is returned by ExemptSessionFromMFAChallenge when the user enrolled their first MFA
	// method after the login decided that they needed no MFA challenge.
	ErrMFAEnrolledDuringSignIn = errors.New("the user enrolled MFA during sign-in")
)

// WebAuthnCredential is the security key credential a WebAuthn enrollment registers.
type WebAuthnCredential struct {
	Label      string
	ID         []byte
	PublicKey  []byte
	Transports []string
	Metadata   []byte
	SignCount  uint32
}

// FinalizeMFAEnrollment confirms the user's MFA method and marks them enrolled, in one transaction that holds the
// user's row lock. For a WebAuthn enrollment, credential is stored on the pending method. The method must still
// exist (ErrMFAMethodNotFound otherwise, and nothing changes): a new enrollment deletes unconfirmed methods.
//
// On a first enrollment, every session of the user loses its MFA verification in the same transaction: they were
// exempt only because the user had no factor. ExemptSessionFromMFAChallenge checks enrollment under the same lock,
// so a racing login can't exempt its new session afterwards. Any device trusted before the user had a factor is
// revoked too: trusting it verified nothing.
func (d *Database) FinalizeMFAEnrollment(
	userID, methodID int64, confirmedAt time.Time, credential *WebAuthnCredential,
) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin MFA enrollment: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	enrolled, err := d.lockUserMFAEnrollmentTx(tx, userID)
	if err != nil {
		return err
	}
	if err := d.confirmMethodTx(tx, userID, methodID, confirmedAt, credential); err != nil {
		return err
	}
	if _, err := d.execTx(tx, `UPDATE users SET mfa_enrolled = TRUE,
		mfa_enforced_at = COALESCE(mfa_enforced_at, NOW()), updated_at = NOW() WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("failed to update user MFA enrollment: %w", err)
	}
	if !enrolled {
		if _, err := d.execTx(tx, `UPDATE user_sessions SET mfa_verified_at = NULL, recent_step_up_at = NULL,
			updated_at = NOW() WHERE user_id = ? AND revoked_at IS NULL`, userID); err != nil {
			return fmt.Errorf("failed to clear session MFA verification: %w", err)
		}
		if err := d.revokeTrustedDevicesTx(tx, userID, "first_mfa_enrollment"); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit MFA enrollment: %w", err)
	}
	return nil
}

// DeleteUserMFAMethod deletes one of the user's MFA methods (ErrMFAMethodNotFound if they have no such method).
// When it was their last confirmed method, the same transaction marks them not enrolled and revokes their trusted
// devices, which would otherwise skip the MFA challenge once they enroll again. It holds the user's row lock, so an
// enrollment finalized at the same time is counted.
func (d *Database) DeleteUserMFAMethod(userID, methodID int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin MFA method deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.lockUserTx(tx, userID); err != nil {
		return err
	}
	res, err := d.execTx(tx, "DELETE FROM mfa_methods WHERE id = ? AND user_id = ?", methodID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete MFA method: %w", err)
	}
	if err := requireAffectedRow(res, ErrMFAMethodNotFound); err != nil {
		return err
	}
	var remaining int
	err = tx.QueryRow(d.rebind("SELECT COUNT(*) FROM mfa_methods WHERE user_id = ? AND confirmed_at IS NOT NULL"),
		userID).Scan(&remaining)
	if err != nil {
		return fmt.Errorf("failed to count the remaining MFA methods: %w", err)
	}
	if remaining == 0 {
		if _, err := d.execTx(tx, "UPDATE users SET mfa_enrolled = FALSE, updated_at = NOW() WHERE id = ?",
			userID); err != nil {
			return fmt.Errorf("failed to update user MFA enrollment: %w", err)
		}
		if err := d.revokeTrustedDevicesTx(tx, userID, "mfa_disabled"); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit MFA method deletion: %w", err)
	}
	return nil
}

// confirmMethodTx confirms the user's method: a TOTP or Yubico OTP method as it is, or a pending WebAuthn method
// with its credential. It returns ErrMFAMethodNotFound when no such method exists.
func (d *Database) confirmMethodTx(
	tx *sql.Tx, userID, methodID int64, confirmedAt time.Time, credential *WebAuthnCredential,
) error {
	var res sql.Result
	var err error
	if credential == nil {
		res, err = d.execTx(tx, `UPDATE mfa_methods SET confirmed_at = COALESCE(confirmed_at, ?), last_used_at = ?
			WHERE id = ? AND user_id = ? AND type <> 'webauthn_pending'`, confirmedAt, confirmedAt, methodID, userID)
	} else {
		res, err = d.execTx(tx, `UPDATE mfa_methods SET type = 'webauthn', label = ?, credential_id = ?,
			public_key = ?, transports = ?, metadata = ?, webauthn_sign_count = ?, confirmed_at = ?, last_used_at = ?
			WHERE id = ? AND user_id = ? AND type = 'webauthn_pending'`,
			nullableString(credential.Label, 150), credential.ID, credential.PublicKey,
			jsonStringArray(credential.Transports), nullableJSON(credential.Metadata), int64(credential.SignCount),
			confirmedAt, confirmedAt, methodID, userID)
	}
	if err != nil {
		return fmt.Errorf("failed to confirm MFA method: %w", err)
	}
	return requireAffectedRow(res, ErrMFAMethodNotFound)
}

// nullableJSON is raw JSON for a json column, or NULL when there is none.
func nullableJSON(raw []byte) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

// requireAffectedRow returns notFound when res changed no row.
func requireAffectedRow(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check the update: %w", err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}

// ExemptSessionFromMFAChallenge marks a new session MFA-verified without a factor: its user needs no MFA challenge
// (trustedDeviceID is nil), or it presented a trusted-device cookie. It opens no step-up window; only a verified
// factor does. Without a trusted device it checks, under the user's row lock, that the user still has no MFA
// method: a login that decided before the user's first enrollment that no challenge was needed gets
// ErrMFAEnrolledDuringSignIn, and its session has to verify a factor.
func (d *Database) ExemptSessionFromMFAChallenge(sessionID int64, at time.Time, trustedDeviceID *int64) error {
	if trustedDeviceID != nil {
		return d.markSessionMFAVerified(sessionID, at, nil, trustedDeviceID)
	}
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin session MFA exemption: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var enrolled bool
	err = tx.QueryRow(d.rebind(`SELECT u.mfa_enrolled FROM users u JOIN user_sessions s ON s.user_id = u.id
		WHERE s.id = ? FOR SHARE OF u`), sessionID).Scan(&enrolled)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("session %d not found", sessionID)
	}
	if err != nil {
		return fmt.Errorf("failed to check the session's user: %w", err)
	}
	if enrolled {
		return ErrMFAEnrolledDuringSignIn
	}
	if _, err := d.execTx(tx, "UPDATE user_sessions SET mfa_verified_at = ?, updated_at = NOW() WHERE id = ?",
		at, sessionID); err != nil {
		return fmt.Errorf("failed to exempt session from MFA: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit session MFA exemption: %w", err)
	}
	return nil
}
