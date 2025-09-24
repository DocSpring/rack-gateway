package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// CreateUserResource records the creator for a resource. Upserts on (resource_type, resource_id).
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

// GetResourceCreator returns the user_id for a given resource if present.
func (d *Database) GetResourceCreator(resourceType, resourceID string) (int64, bool, error) {
	var uid int64
	err := d.queryRow(`SELECT user_id FROM user_resources WHERE resource_type = ? AND resource_id = ?`, resourceType, resourceID).Scan(&uid)
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
	// Build IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, resourceType)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `
        SELECT ur.resource_id, u.id, u.email, u.name
        FROM user_resources ur
        JOIN users u ON u.id = ur.user_id
        WHERE ur.resource_type = ? AND ur.resource_id IN (` + strings.Join(placeholders, ",") + `)
    `
	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query creators: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup
	for rows.Next() {
		var rid string
		var uid int64
		var email, name string
		if err := rows.Scan(&rid, &uid, &email, &name); err != nil {
			return nil, fmt.Errorf("failed to scan creators: %w", err)
		}
		out[rid] = &CreatorInfo{UserID: uid, Email: email, Name: name}
	}
	return out, nil
}
