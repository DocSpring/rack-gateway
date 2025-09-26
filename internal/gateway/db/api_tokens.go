package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// CreateAPIToken creates a new API token
func (d *Database) CreateAPIToken(tokenHash, name string, userID int64, permissions []string, expiresAt *time.Time, createdByUserID *int64) (*APIToken, error) {
	permissionsJSON, err := json.Marshal(permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal permissions: %w", err)
	}

	var expVal interface{}
	if expiresAt != nil {
		expVal = *expiresAt
	} else {
		expVal = nil
	}

	var id int64
	if err := d.queryRow("INSERT INTO api_tokens (token_hash, name, user_id, permissions, expires_at, created_by_user_id) VALUES (?, ?, ?, ?, ?, ?) RETURNING id", tokenHash, name, userID, string(permissionsJSON), expVal, createdByUserID).Scan(&id); err != nil {
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}
	return &APIToken{
		ID:              id,
		TokenHash:       tokenHash,
		Name:            name,
		UserID:          userID,
		CreatedByUserID: createdByUserID,
		Permissions:     permissions,
		CreatedAt:       time.Now(),
		ExpiresAt:       expiresAt,
	}, nil
}

// GetAPITokenByHash retrieves an API token by its hash
func (d *Database) GetAPITokenByHash(tokenHash string) (*APIToken, error) {
	var token APIToken
	var permissionsJSON string
	var expiresAtNull sql.NullTime
	var lastUsedAtNull sql.NullTime

	var createdByNull sql.NullInt64
	err := d.queryRow(
		"SELECT id, token_hash, name, user_id, permissions, created_at, expires_at, last_used_at, created_by_user_id FROM api_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID, &permissionsJSON,
		&token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API token: %w", err)
	}

	if err := json.Unmarshal([]byte(permissionsJSON), &token.Permissions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
	}

	if expiresAtNull.Valid {
		t := expiresAtNull.Time
		token.ExpiresAt = &t
	}
	if lastUsedAtNull.Valid {
		token.LastUsedAt = &lastUsedAtNull.Time
	}
	if createdByNull.Valid {
		v := createdByNull.Int64
		token.CreatedByUserID = &v
	}

	return &token, nil
}

// GetAPITokenByID retrieves an API token by ID
func (d *Database) GetAPITokenByID(id int64) (*APIToken, error) {
	var token APIToken
	var permissionsJSON string
	var expiresAtNull sql.NullTime
	var lastUsedAtNull sql.NullTime
	var createdByNull sql.NullInt64
	var createdByEmail sql.NullString
	var createdByName sql.NullString

	row := d.queryRow(
		"SELECT t.id, t.token_hash, t.name, t.user_id, t.permissions, t.created_at, t.expires_at, t.last_used_at, t.created_by_user_id, cu.email, cu.name FROM api_tokens t LEFT JOIN users cu ON cu.id = t.created_by_user_id WHERE t.id = ?",
		id,
	)
	err := row.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID, &permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull, &createdByEmail, &createdByName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API token: %w", err)
	}

	if err := json.Unmarshal([]byte(permissionsJSON), &token.Permissions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
	}
	if expiresAtNull.Valid {
		v := expiresAtNull.Time
		token.ExpiresAt = &v
	}
	if lastUsedAtNull.Valid {
		v := lastUsedAtNull.Time
		token.LastUsedAt = &v
	}
	if createdByNull.Valid {
		v := createdByNull.Int64
		token.CreatedByUserID = &v
	}
	if createdByEmail.Valid {
		token.CreatedByEmail = createdByEmail.String
	}
	if createdByName.Valid {
		token.CreatedByName = createdByName.String
	}

	return &token, nil
}

// APITokenNameExists reports whether a token name is already taken.
func (d *Database) APITokenNameExists(name string, excludeID int64) (bool, error) {
	query := "SELECT 1 FROM api_tokens WHERE name = ?"
	args := []interface{}{name}
	if excludeID > 0 {
		query += " AND id <> ?"
		args = append(args, excludeID)
	}
	var exists int
	err := d.queryRow(query, args...).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check token name: %w", err)
	}
	return true, nil
}

// ListAPITokensByUser returns all API tokens for a user
func (d *Database) ListAPITokensByUser(userID int64) ([]*APIToken, error) {
	rows, err := d.query(
		"SELECT t.id, t.token_hash, t.name, t.user_id, t.permissions, t.created_at, t.expires_at, t.last_used_at, t.created_by_user_id, cu.email, cu.name FROM api_tokens t LEFT JOIN users cu ON cu.id = t.created_by_user_id WHERE t.user_id = ? ORDER BY t.created_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list API tokens: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var tokens []*APIToken
	for rows.Next() {
		var token APIToken
		var permissionsJSON string
		var expiresAtNull sql.NullTime
		var lastUsedAtNull sql.NullTime
		var createdByNull sql.NullInt64
		var createdByEmail sql.NullString
		var createdByName sql.NullString

		err := rows.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID,
			&permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull, &createdByEmail, &createdByName)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API token: %w", err)
		}

		if err := json.Unmarshal([]byte(permissionsJSON), &token.Permissions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
		}

		if expiresAtNull.Valid {
			t := expiresAtNull.Time
			token.ExpiresAt = &t
		}
		if lastUsedAtNull.Valid {
			token.LastUsedAt = &lastUsedAtNull.Time
		}
		if createdByNull.Valid {
			v := createdByNull.Int64
			token.CreatedByUserID = &v
		}
		if createdByEmail.Valid {
			token.CreatedByEmail = createdByEmail.String
		}
		if createdByName.Valid {
			token.CreatedByName = createdByName.String
		}

		tokens = append(tokens, &token)
	}

	return tokens, nil
}

// ListAllAPITokens returns all API tokens with creator metadata
func (d *Database) ListAllAPITokens() ([]*APIToken, error) {
	rows, err := d.query(
		"SELECT t.id, t.token_hash, t.name, t.user_id, t.permissions, t.created_at, t.expires_at, t.last_used_at, t.created_by_user_id, cu.email, cu.name FROM api_tokens t LEFT JOIN users cu ON cu.id = t.created_by_user_id ORDER BY t.created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list API tokens: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var tokens []*APIToken
	for rows.Next() {
		var token APIToken
		var permissionsJSON string
		var expiresAtNull sql.NullTime
		var lastUsedAtNull sql.NullTime
		var createdByNull sql.NullInt64
		var createdByEmail sql.NullString
		var createdByName sql.NullString

		err := rows.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID,
			&permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull, &createdByEmail, &createdByName)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API token: %w", err)
		}

		if err := json.Unmarshal([]byte(permissionsJSON), &token.Permissions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
		}

		if expiresAtNull.Valid {
			t := expiresAtNull.Time
			token.ExpiresAt = &t
		}
		if lastUsedAtNull.Valid {
			token.LastUsedAt = &lastUsedAtNull.Time
		}
		if createdByNull.Valid {
			v := createdByNull.Int64
			token.CreatedByUserID = &v
		}
		if createdByEmail.Valid {
			token.CreatedByEmail = createdByEmail.String
		}
		if createdByName.Valid {
			token.CreatedByName = createdByName.String
		}

		tokens = append(tokens, &token)
	}

	return tokens, nil
}

// UpdateAPITokenLastUsed updates the last used timestamp
func (d *Database) UpdateAPITokenLastUsed(tokenHash string) error {
	_, err := d.exec(
		"UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE token_hash = ?",
		tokenHash,
	)
	if err != nil {
		return fmt.Errorf("failed to update API token last used: %w", err)
	}
	return nil
}

// DeleteAPIToken removes an API token
func (d *Database) DeleteAPIToken(id int64) error {
	_, err := d.exec("DELETE FROM api_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete API token: %w", err)
	}
	return nil
}

// UpdateAPITokenName renames an existing API token
func (d *Database) UpdateAPITokenName(id int64, name string) error {
	_, err := d.exec("UPDATE api_tokens SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("failed to update API token name: %w", err)
	}
	return nil
}

// UpdateAPITokenPermissions replaces the permission set for an API token
func (d *Database) UpdateAPITokenPermissions(id int64, permissions []string) error {
	permsJSON, err := json.Marshal(permissions)
	if err != nil {
		return fmt.Errorf("failed to marshal permissions: %w", err)
	}
	_, err = d.exec("UPDATE api_tokens SET permissions = ? WHERE id = ?", string(permsJSON), id)
	if err != nil {
		return fmt.Errorf("failed to update API token permissions: %w", err)
	}
	return nil
}
