package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// GetUserByID retrieves a user by ID
func (d *Database) GetUserByID(id int64) (*User, error) {
	row := d.queryRow(
		`SELECT id, email, name, roles, created_at, updated_at, mfa_enrolled,
			mfa_enforced_at, preferred_mfa_method, locked_at, locked_reason, locked_by_user_id
		FROM users WHERE id = ?`,
		id,
	)

	user, fields, err := scanUserBasicFields(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := finalizeUserFields(user, fields); err != nil {
		return nil, err
	}

	return user, nil
}

// GetUser retrieves a user by email
func (d *Database) GetUser(email string) (*User, error) {
	row := d.queryRow(
		`SELECT u.id, u.email, u.name, u.roles, u.created_at, u.updated_at,
			u.mfa_enrolled, u.mfa_enforced_at, u.preferred_mfa_method,
			u.locked_at, u.locked_reason, u.locked_by_user_id,
			lbu.email, lbu.name
		FROM users u
		LEFT JOIN users lbu ON lbu.id = u.locked_by_user_id
		WHERE u.email = ?`,
		email,
	)

	user, err := scanUserWithLockedBy(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// CreateUser creates a new user
func (d *Database) CreateUser(email, name string, roles []string) (*User, error) {
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal roles: %w", err)
	}

	var id int64
	query := "INSERT INTO users (email, name, roles) VALUES (?, ?, ?) RETURNING id"
	err = d.queryRow(query, email, name, string(rolesJSON)).Scan(&id)
	if isUniqueViolation(err) {
		return nil, ErrUserExists
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &User{
		ID:        id,
		Email:     email,
		Name:      name,
		Roles:     roles,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// DeleteUser deletes the user with the email, revokes their sessions and removes the record of who created them, in
// one transaction. The user's row is locked first and deleted by its ID, so an email change made at the same time
// can't make it delete another user. It returns ErrUserNotFound when no user has the email.
func (d *Database) DeleteUser(email string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin user deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	err = tx.QueryRow(d.rebind("SELECT id FROM users WHERE email = ? FOR UPDATE"), email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to lock user %s: %w", email, err)
	}
	count, err := d.revokeUserSessionsTx(tx, id, nil)
	if err != nil {
		return err
	}
	res, err := d.execTx(tx, "DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	if deleted, err := res.RowsAffected(); err != nil || deleted != 1 {
		return fmt.Errorf("failed to delete user %s: %d rows deleted (%v)", email, deleted, err)
	}
	// A user created later with the same email has its own creator.
	_, err = d.execTx(tx, "DELETE FROM user_resources WHERE resource_type = 'user' AND resource_id = ?", email)
	if err != nil {
		return fmt.Errorf("failed to delete the user's creator: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit user deletion: %w", err)
	}
	log.Printf("Revoked %d sessions for user %s during deletion", count, email)
	return nil
}

// ListUsers returns all users
func (d *Database) ListUsers() ([]*User, error) {
	rows, err := d.query(
		`SELECT u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.mfa_enrolled,
			u.locked_at, u.locked_reason, u.locked_by_user_id,
			cu.id, cu.email, cu.name
		FROM users u
		LEFT JOIN user_resources ur ON ur.resource_type = 'user' AND ur.resource_id = u.email
		LEFT JOIN users cu ON cu.id = ur.user_id
		ORDER BY u.created_at DESC, u.email`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []*User
	for rows.Next() {
		user, err := scanUserWithCreator(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	return users, nil
}

// FillUserName sets a user's display name only when it's empty, so a name set in the gateway is never overwritten.
// It reports whether the name was set.
func (d *Database) FillUserName(email, name string) (bool, error) {
	res, err := d.exec(
		"UPDATE users SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ? AND name = ''",
		name,
		email,
	)
	if err != nil {
		return false, fmt.Errorf("failed to fill user name: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to fill user name: %w", err)
	}
	return rows > 0, nil
}

// UpdatePreferredMFAMethod updates a user's preferred MFA method
func (d *Database) UpdatePreferredMFAMethod(userID int64, method *string) error {
	if method != nil && *method != "totp" && *method != "webauthn" {
		return fmt.Errorf("invalid MFA method: %s", *method)
	}
	_, err := d.exec(
		"UPDATE users SET preferred_mfa_method = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		method,
		userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update preferred MFA method: %w", err)
	}
	return nil
}
