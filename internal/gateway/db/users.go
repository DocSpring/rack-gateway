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

	err := d.queryRow(
		"SELECT id, email, name, roles, created_at, updated_at, suspended FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	return &user, nil
}

// GetUser retrieves a user by email
func (d *Database) GetUser(email string) (*User, error) {
	var user User
	var rolesJSON string

	err := d.queryRow(
		"SELECT id, email, name, roles, created_at, updated_at, suspended FROM users WHERE email = ?",
		email,
	).Scan(&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
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
	var uid sql.NullInt64
	if err := d.queryRow("SELECT id FROM users WHERE email = ?", email).Scan(&uid); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("failed to load user id for audit cleanup: %w", err)
	}
	if !uid.Valid {
		return nil
	}
	if _, err := d.exec("DELETE FROM audit_logs WHERE user_email = ?", email); err != nil {
		return fmt.Errorf("failed to delete audit logs for user: %w", err)
	}
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
			cu.id, cu.email, cu.name
		FROM users u
		LEFT JOIN user_resources ur ON ur.resource_type = 'user' AND ur.resource_id = u.email
		LEFT JOIN users cu ON cu.id = ur.user_id
		ORDER BY u.created_at DESC, u.email`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		var rolesJSON string
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
