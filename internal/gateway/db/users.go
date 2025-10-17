package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// GetUserByID retrieves a user by ID
func (d *Database) GetUserByID(id int64) (*User, error) {
	var user User
	var rolesJSON string
	var lockedReason sql.NullString

	err := d.queryRow(
		"SELECT id, email, name, roles, created_at, updated_at, suspended, mfa_enrolled, mfa_enforced_at, preferred_mfa_method, locked_at, locked_reason, locked_by_user_id FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended, &user.MFAEnrolled, &user.MFAEnforcedAt, &user.PreferredMFAMethod, &user.LockedAt, &lockedReason, &user.LockedByUserID)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	if lockedReason.Valid {
		user.LockedReason = lockedReason.String
	}

	return &user, nil
}

// GetUser retrieves a user by email
func (d *Database) GetUser(email string) (*User, error) {
	var user User
	var rolesJSON string
	var lockedReason sql.NullString
	var lockedByEmail, lockedByName sql.NullString

	err := d.queryRow(
		`SELECT u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.suspended, u.mfa_enrolled, u.mfa_enforced_at, u.preferred_mfa_method,
			u.locked_at, u.locked_reason, u.locked_by_user_id,
			lbu.email, lbu.name
		FROM users u
		LEFT JOIN users lbu ON lbu.id = u.locked_by_user_id
		WHERE u.email = ?`,
		email,
	).Scan(&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended, &user.MFAEnrolled, &user.MFAEnforcedAt, &user.PreferredMFAMethod, &user.LockedAt, &lockedReason, &user.LockedByUserID, &lockedByEmail, &lockedByName)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	if lockedReason.Valid {
		user.LockedReason = lockedReason.String
	}
	if lockedByEmail.Valid {
		user.LockedByEmail = lockedByEmail.String
	}
	if lockedByName.Valid {
		user.LockedByName = lockedByName.String
	}

	return &user, nil
}

// CreateUser creates a new user
func (d *Database) CreateUser(email, name string, roles []string) (*User, error) {
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal roles: %w", err)
	}

	var id int64
	if err := d.queryRow("INSERT INTO users (email, name, roles) VALUES (?, ?, ?) RETURNING id", email, name, string(rolesJSON)).Scan(&id); err != nil {
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

// DeleteUser removes a user from the database
func (d *Database) deleteUserAuditLogs(email string) error {
	// Audit logs are immutable and should never be deleted
	// The audit.audit_event table has triggers that block DELETE operations
	// We preserve audit trail even after user deletion for compliance
	return nil
}

func (d *Database) DeleteUser(email string) error {
	user, err := d.GetUser(email)
	if err != nil {
		return err
	}

	if err := d.deleteUserAuditLogs(email); err != nil {
		return err
	}

	if user != nil {
		if _, err := d.RevokeAllUserSessions(user.ID, nil); err != nil {
			return fmt.Errorf("failed to revoke sessions for user %s: %w", email, err)
		}
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
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	var users []*User
	for rows.Next() {
		var user User
		var rolesJSON string
		var lockedReason sql.NullString
		var creatorID sql.NullInt64
		var creatorEmail sql.NullString
		var creatorName sql.NullString

		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.Name,
			&rolesJSON,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.Suspended,
			&user.LockedAt,
			&lockedReason,
			&user.LockedByUserID,
			&creatorID,
			&creatorEmail,
			&creatorName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}

		if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
			return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
		}

		if lockedReason.Valid {
			user.LockedReason = lockedReason.String
		}

		if creatorID.Valid {
			id := creatorID.Int64
			user.CreatedByUserID = &id
		}
		if creatorEmail.Valid {
			user.CreatedByEmail = creatorEmail.String
		}
		if creatorName.Valid {
			user.CreatedByName = creatorName.String
		}

		users = append(users, &user)
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

	if _, err := d.RevokeAllUserSessions(user.ID, nil); err != nil {
		return fmt.Errorf("failed to revoke sessions for user %s: %w", oldEmail, err)
	}

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
	_, err := d.exec("UPDATE users SET preferred_mfa_method = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", method, userID)
	if err != nil {
		return fmt.Errorf("failed to update preferred MFA method: %w", err)
	}
	return nil
}
