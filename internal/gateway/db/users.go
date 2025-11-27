package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// GetUserByID retrieves a user by ID
func (d *Database) GetUserByID(id int64) (*User, error) {
	row := d.queryRow(
		`SELECT id, email, name, roles, created_at, updated_at, suspended, mfa_enrolled,
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
		`SELECT u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.suspended,
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
	if err := d.queryRow(query, email, name, string(rolesJSON)).Scan(&id); err != nil {
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

// UpdateUserRoles updates a user's roles
func (d *Database) UpdateUserRoles(email string, roles []string) error {
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return fmt.Errorf("failed to marshal roles: %w", err)
	}

	_, err = d.exec(
		"UPDATE users SET roles = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ?",
		string(rolesJSON), email,
	)
	if err != nil {
		return fmt.Errorf("failed to update user roles: %w", err)
	}

	return nil
}

// deleteUserAuditLogs is a no-op that preserves audit logs for compliance.
// Audit logs are immutable and should never be deleted.
// The audit.audit_event table has triggers that block DELETE operations.
func (_ *Database) deleteUserAuditLogs(_ string) error {
	return nil
}

// DeleteUser removes a user from the database and revokes all their sessions.
func (d *Database) DeleteUser(email string) error {
	user, err := d.GetUser(email)
	if err != nil {
		return err
	}

	if err := d.deleteUserAuditLogs(email); err != nil {
		return err
	}

	if user != nil {
		count, err := d.RevokeAllUserSessions(user.ID, nil)
		if err != nil {
			return fmt.Errorf("failed to revoke sessions for user %s: %w", email, err)
		}
		log.Printf("Revoked %d sessions for user %s during deletion", count, email)
	}

	_, err = d.exec("DELETE FROM users WHERE email = ?", email)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// ListUsers returns all users
func (d *Database) ListUsers() ([]*User, error) {
	rows, err := d.query(
		`SELECT u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.suspended,
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

	return users, nil
}

// UpdateUserName updates a user's display name by email
func (d *Database) UpdateUserName(email, name string) error {
	_, err := d.exec("UPDATE users SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ?", name, email)
	if err != nil {
		return fmt.Errorf("failed to update user name: %w", err)
	}
	return nil
}

// UpdateUserEmail updates a user's email address
func (d *Database) UpdateUserEmail(oldEmail, newEmail string) error {
	user, err := d.GetUser(oldEmail)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user %s not found", oldEmail)
	}

	count, err := d.RevokeAllUserSessions(user.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to revoke sessions for user %s: %w", oldEmail, err)
	}
	log.Printf("Revoked %d sessions for user %s during email change", count, oldEmail)

	_, err = d.exec("UPDATE users SET email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", newEmail, user.ID)
	if err != nil {
		return fmt.Errorf("failed to update user email: %w", err)
	}
	return nil
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
