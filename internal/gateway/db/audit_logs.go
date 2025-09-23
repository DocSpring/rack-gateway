package db

import (
	"fmt"
	"time"
)

// CreateAuditLog creates a new audit log entry
func (d *Database) CreateAuditLog(log *AuditLog) error {
	_, err := d.exec(
		`INSERT INTO audit_logs (
            user_email, user_name, action_type, action, command, resource, resource_type,
            details, ip_address, user_agent, status, rbac_decision, http_status, response_time_ms
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::inet, ?, ?, ?, ?, ?)`,
		log.UserEmail, log.UserName, log.ActionType, log.Action, log.Command, log.Resource, log.ResourceType,
		log.Details, nullableIP(log.IPAddress), log.UserAgent, log.Status, log.RBACDecision, log.HTTPStatus, log.ResponseTimeMs,
	)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}
	return nil
}

// GetAuditLogs retrieves audit logs with optional filters
func (d *Database) GetAuditLogs(userEmail string, since time.Time, limit int) ([]*AuditLog, error) {
	query := `
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''), "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms"
        FROM "audit_logs"
        WHERE 1=1
    `
	args := []interface{}{}

	if userEmail != "" {
		query += " AND \"user_email\" = ?"
		args = append(args, userEmail)
	}

	if !since.IsZero() {
		query += " AND \"timestamp\" >= ?"
		args = append(args, since.UTC())
	}

	query += " ORDER BY \"timestamp\" DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName,
			&log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details,
			&log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// AuditLogFilters contains all possible filters for audit logs
type AuditLogFilters struct {
	UserEmail    string
	Status       string
	ActionType   string
	ResourceType string
	Search       string
	Since        time.Time
	Until        time.Time
	Limit        int
	Offset       int
}

// GetAuditLogsPaged retrieves audit logs with proper SQL filtering and pagination
func (d *Database) GetAuditLogsPaged(filters AuditLogFilters) ([]*AuditLog, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}

	// Build WHERE clause with proper SQL filtering
	whereClause := "WHERE 1=1"
	args := []interface{}{}

	if filters.UserEmail != "" {
		whereClause += " AND \"user_email\" = ?"
		args = append(args, filters.UserEmail)
	}
	if filters.Status != "" && filters.Status != "all" {
		whereClause += " AND \"status\" = ?"
		args = append(args, filters.Status)
	}
	if filters.ActionType != "" && filters.ActionType != "all" {
		whereClause += " AND \"action_type\" = ?"
		args = append(args, filters.ActionType)
	}
	if filters.ResourceType != "" && filters.ResourceType != "all" {
		whereClause += " AND \"resource_type\" = ?"
		args = append(args, filters.ResourceType)
	}
	if !filters.Since.IsZero() {
		whereClause += " AND \"timestamp\" >= ?"
		args = append(args, filters.Since.UTC())
	}
	if !filters.Until.IsZero() {
		whereClause += " AND \"timestamp\" <= ?"
		args = append(args, filters.Until.UTC())
	}

	// Full-text search across multiple columns
	if filters.Search != "" {
		whereClause += ` AND (
            "user_email" ILIKE ? OR
            "user_name" ILIKE ? OR
            "action" ILIKE ? OR
            "resource" ILIKE ? OR
            "details" ILIKE ? OR
            host("ip_address"::inet) ILIKE ? OR
            "user_agent" ILIKE ?
        )`
		searchPattern := "%" + filters.Search + "%"
		for i := 0; i < 7; i++ {
			args = append(args, searchPattern)
		}
	}

	// Get total count for pagination - build query safely
	countQuery := "SELECT COUNT(*) FROM \"audit_logs\" " + whereClause
	var total int
	if err := d.queryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Get paginated results - build query safely
	query := `
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''),
               COALESCE("details", ''), COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms"
        FROM "audit_logs" ` + whereClause + `
        ORDER BY "timestamp" DESC
		LIMIT ? OFFSET ?`

	args = append(args, filters.Limit, filters.Offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details, &log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs); err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, &log)
	}
	return logs, total, nil
}

// CleanupOldAuditLogs deletes audit logs older than retentionDays
func (d *Database) CleanupOldAuditLogs(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	_, err := d.exec("DELETE FROM audit_logs WHERE timestamp < NOW() - (INTERVAL '1 day' * ?::int)", retentionDays)
	return err
}
