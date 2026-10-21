package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrUserNotFound is returned when the user to change doesn't exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrUserExists is returned when a new user, or a user's new email, would duplicate another user's email.
	ErrUserExists = errors.New("a user with that email already exists")
)

// UserUpdate is a change to a user. A nil field is left as it is.
type UserUpdate struct {
	Email *string
	Name  *string
	Roles []string
}

// UserUpdateResult is what UpdateUser changed.
type UserUpdateResult struct {
	User            *User
	EmailChanged    bool
	RevokedSessions int64
	DeletedTokens   int64
}

// UpdateUser applies update to the user with id in one transaction, which holds the user's row lock: a concurrent
// change can't interleave with it, and only the fields the update names are written, so a name change never
// writes back roles another admin just changed.
//
// A new email can hand the account to someone else, so changing it also revokes all of the user's sessions and
// deletes their API tokens: nothing issued to the old address keeps working. A login racing the change can't add a
// session afterwards (CreateUserSession checks the email under the same row lock).
func (d *Database) UpdateUser(id int64, update UserUpdate, changedBy *int64) (*UserUpdateResult, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin user update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentEmail string
	err = tx.QueryRow(d.rebind("SELECT email FROM users WHERE id = ? FOR UPDATE"), id).Scan(&currentEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load user: %w", err)
	}

	result := &UserUpdateResult{}
	if update.Email != nil && !strings.EqualFold(*update.Email, currentEmail) {
		if err := d.changeUserEmailTx(tx, id, currentEmail, *update.Email, changedBy, result); err != nil {
			return nil, err
		}
	}
	if err := d.updateUserProfileTx(tx, id, update); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit user update: %w", err)
	}

	result.User, err = d.GetUserByID(id)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// updateUserProfileTx writes the update's name and roles, when it has them.
func (d *Database) updateUserProfileTx(tx *sql.Tx, id int64, update UserUpdate) error {
	if update.Name != nil {
		if _, err := d.execTx(tx, "UPDATE users SET name = ?, updated_at = NOW() WHERE id = ?",
			*update.Name, id); err != nil {
			return fmt.Errorf("failed to update user name: %w", err)
		}
	}
	if update.Roles == nil {
		return nil
	}
	rolesJSON, err := json.Marshal(update.Roles)
	if err != nil {
		return fmt.Errorf("failed to marshal roles: %w", err)
	}
	if _, err := d.execTx(tx, "UPDATE users SET roles = ?, updated_at = NOW() WHERE id = ?",
		string(rolesJSON), id); err != nil {
		return fmt.Errorf("failed to update user roles: %w", err)
	}
	return nil
}

// changeUserEmailTx changes the user's email, then revokes their sessions and deletes their API tokens. The record
// of who created the user moves to the new email.
func (d *Database) changeUserEmailTx(
	tx *sql.Tx, id int64, oldEmail, email string, changedBy *int64, result *UserUpdateResult,
) error {
	_, err := d.execTx(tx, "UPDATE users SET email = ?, updated_at = NOW() WHERE id = ?", email, id)
	if isUniqueViolation(err) {
		return ErrUserExists
	}
	if err != nil {
		return fmt.Errorf("failed to update user email: %w", err)
	}
	result.EmailChanged = true

	if _, err := d.execTx(tx, "DELETE FROM user_resources WHERE resource_type = 'user' AND resource_id = ?",
		email); err != nil {
		return fmt.Errorf("failed to update the user's creator: %w", err)
	}
	if _, err := d.execTx(tx, `UPDATE user_resources SET resource_id = ?
		WHERE resource_type = 'user' AND resource_id = ?`, email, oldEmail); err != nil {
		return fmt.Errorf("failed to update the user's creator: %w", err)
	}

	if result.RevokedSessions, err = d.revokeUserSessionsTx(tx, id, changedBy); err != nil {
		return err
	}

	deleted, err := d.execTx(tx, "DELETE FROM api_tokens WHERE user_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete the user's API tokens: %w", err)
	}
	if result.DeletedTokens, err = deleted.RowsAffected(); err != nil {
		return fmt.Errorf("failed to delete the user's API tokens: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a unique constraint violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
