package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrSignInAccountMismatch is returned when an account signs in with a user's email but isn't the account bound to
// the user, or is bound to another user.
var ErrSignInAccountMismatch = errors.New("this account isn't the one bound to the user")

// BindUserSignInAccount checks that subject, the identity provider's ID of the account signing in, belongs to the
// user. The user's first sign-in binds the account to them. After that, an account with another subject gets
// ErrSignInAccountMismatch, so an email address reassigned to someone else doesn't inherit the user. So does an
// account already bound to another user.
func (d *Database) BindUserSignInAccount(userID int64, subject string) error {
	if strings.TrimSpace(subject) == "" {
		return ErrSignInAccountMismatch
	}
	res, err := d.exec(`UPDATE users SET oidc_subject = ?, updated_at = NOW()
		WHERE id = ? AND oidc_subject IS NULL`, subject, userID)
	if isUniqueViolation(err) {
		return ErrSignInAccountMismatch
	}
	if err != nil {
		return fmt.Errorf("failed to bind the user's sign-in account: %w", err)
	}
	bound, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to bind the user's sign-in account: %w", err)
	}
	if bound == 1 {
		return nil
	}

	var current sql.NullString
	err = d.queryRow("SELECT oidc_subject FROM users WHERE id = ?", userID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to read the user's sign-in account: %w", err)
	}
	if current.String != subject {
		return ErrSignInAccountMismatch
	}
	return nil
}

// ResetUserSignInAccount unbinds the user's sign-in account, so the next account to sign in with their email is
// bound to them, and revokes their sessions (revokedBy is the admin who reset it). Their MFA methods and API tokens
// are kept. It returns ErrUserNotFound when there's no such user.
func (d *Database) ResetUserSignInAccount(userID int64, revokedBy *int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin sign-in account reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.lockUserTx(tx, userID); err != nil {
		return err
	}
	if _, err := d.execTx(tx, `UPDATE users SET oidc_subject = NULL, updated_at = NOW() WHERE id = ?`,
		userID); err != nil {
		return fmt.Errorf("failed to reset the user's sign-in account: %w", err)
	}
	if _, err := d.revokeUserSessionsTx(tx, userID, revokedBy); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit sign-in account reset: %w", err)
	}
	return nil
}
