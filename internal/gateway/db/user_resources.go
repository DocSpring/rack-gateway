package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// CreateUserResource records the creator of a resource unless one is recorded already, and reports whether it
// recorded one.
func (d *Database) CreateUserResource(userID int64, resourceType, resourceID string) (bool, error) {
	if strings.TrimSpace(resourceType) == "" || strings.TrimSpace(resourceID) == "" {
		return false, fmt.Errorf("invalid resource: type and id required")
	}
	res, err := d.exec(`
        INSERT INTO user_resources (user_id, resource_type, resource_id)
        VALUES (?, ?, ?)
        ON CONFLICT (resource_type, resource_id) DO NOTHING
    `, userID, resourceType, resourceID)
	if err != nil {
		return false, fmt.Errorf("failed to create user_resource: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect user_resource insert: %w", err)
	}
	return rows > 0, nil
}

// SetResourceCreator records userID as the creator of a resource that was just created. It replaces the record of
// an earlier resource with the same ID, such as an app deleted and created again.
func (d *Database) SetResourceCreator(userID int64, resourceType, resourceID string) error {
	if strings.TrimSpace(resourceType) == "" || strings.TrimSpace(resourceID) == "" {
		return fmt.Errorf("invalid resource: type and id required")
	}
	_, err := d.exec(`
        INSERT INTO user_resources (user_id, resource_type, resource_id)
        VALUES (?, ?, ?)
        ON CONFLICT (resource_type, resource_id) DO UPDATE SET user_id = EXCLUDED.user_id
    `, userID, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to set user_resource: %w", err)
	}
	return nil
}

// GetResourceCreator returns the user_id for a given resource if present.
func (d *Database) GetResourceCreator(resourceType, resourceID string) (int64, bool, error) {
	var uid int64
	query := `SELECT user_id FROM user_resources WHERE resource_type = ? AND resource_id = ?`
	err := d.queryRow(query, resourceType, resourceID).Scan(&uid)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to query user_resource: %w", err)
	}
	return uid, true, nil
}

// GetResourceCreators returns a map of resource_id -> creator info for the given IDs.
func (d *Database) GetResourceCreators(resourceType string, ids []string) (map[string]*CreatorInfo, error) {
	out := make(map[string]*CreatorInfo)
	if len(ids) == 0 {
		return out, nil
	}
	query := `
        SELECT ur.resource_id, u.id, u.email, u.name
        FROM user_resources ur
        JOIN users u ON u.id = ur.user_id
        WHERE ur.resource_type = ? AND ur.resource_id = ANY(?)
    `
	rows, err := d.query(query, resourceType, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("failed to query creators: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var rid string
		var uid int64
		var email, name string
		if err := rows.Scan(&rid, &uid, &email, &name); err != nil {
			return nil, fmt.Errorf("failed to scan creators: %w", err)
		}
		out[rid] = &CreatorInfo{UserID: uid, Email: email, Name: name}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to query creators: %w", err)
	}
	return out, nil
}
