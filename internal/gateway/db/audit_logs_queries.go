package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AuditLogFilters contains all possible filters for audit logs
type AuditLogFilters struct {
	UserEmail    string
	Status       string
	ActionType   string
	ResourceType string
	Search       string
	Range        string
	Since        time.Time
	Until        time.Time
	Limit        int
	Offset       int
}

// buildAuditFilterWhereClause builds the WHERE clause and args for audit log queries
// timestampColumn should be "timestamp" for non-aggregated or "last_seen" for aggregated views
func buildAuditFilterWhereClause(filters AuditLogFilters, timestampColumn string) (string, []interface{}) {
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
		whereClause += fmt.Sprintf(" AND \"%s\" >= ?", timestampColumn)
		args = append(args, filters.Since.UTC())
	}
	if !filters.Until.IsZero() {
		whereClause += fmt.Sprintf(" AND \"%s\" <= ?", timestampColumn)
		args = append(args, filters.Until.UTC())
	}

	// Full-text search across multiple columns
	if filters.Search != "" {
		searchPattern := "%" + filters.Search + "%"
		searchColumns := []struct {
			expression string
			comparator string
		}{
			{`"user_email"`, "ILIKE"},
			{`"user_name"`, "ILIKE"},
			{`"api_token_name"`, "ILIKE"},
			{`"action"`, "ILIKE"},
			{`"resource"`, "ILIKE"},
			{`"details"`, "ILIKE"},
			{`host("ip_address"::inet)`, "LIKE"},
			{`"user_agent"`, "ILIKE"},
		}

		orClauses := make([]string, 0, len(searchColumns))
		for _, col := range searchColumns {
			orClauses = append(orClauses, fmt.Sprintf("%s %s ?", col.expression, col.comparator))
			args = append(args, searchPattern)
		}
		whereClause += " AND (" + strings.Join(orClauses, " OR ") + ")"
	}

	return whereClause, args
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
	whereClause, args := buildAuditFilterWhereClause(filters, "timestamp")

	// Get total count for pagination
	countQuery := "SELECT COUNT(*) FROM \"audit\".\"audit_event\" " + whereClause
	var total int
	if err := d.queryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Get paginated results
	query := `
        SELECT "id", "timestamp", "chain_index", "previous_hash", "event_hash", "checkpoint_id", "checkpoint_hash",
               "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name", "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''),
               COALESCE("details", ''), COALESCE("request_id", ''), COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms", "event_count", "deploy_approval_request_id"
        FROM "audit"."audit_event" ` + whereClause + `
        ORDER BY "timestamp" DESC
		LIMIT ? OFFSET ?`

	args = append(args, filters.Limit, filters.Offset)

	logs, err := d.queryAuditLogs(query, args)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// queryAuditLogs executes a query and scans the results into AuditLog structs
func (d *Database) queryAuditLogs(query string, args []interface{}) ([]*AuditLog, error) {
	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	return scanAuditLogs(rows)
}

// GetAuditLogsByDeployApprovalRequestID retrieves audit logs for a specific deploy approval request
func (d *Database) GetAuditLogsByDeployApprovalRequestID(
	deployApprovalRequestID int64,
	limit int,
) ([]*AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
        SELECT "id", "timestamp", "chain_index", "previous_hash", "event_hash", "checkpoint_id", "checkpoint_hash",
               "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name", "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE("request_id", ''), COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms",
               "event_count", "deploy_approval_request_id"
        FROM "audit"."audit_event"
        WHERE "deploy_approval_request_id" = ?
        ORDER BY "timestamp" DESC
        LIMIT ?
    `

	rows, err := d.query(query, deployApprovalRequestID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs by deploy approval request ID: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	return scanAuditLogs(rows)
}

func scanAuditLogs(rows *sql.Rows) ([]*AuditLog, error) {
	var logs []*AuditLog
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// VerifyChainIntegrity verifies the cryptographic chain integrity
// Returns nil if chain is valid, error describing the first break if invalid
func (d *Database) VerifyChainIntegrity(startIndex, endIndex int64) error {
	query := `SELECT broken_at_index, event_id, error_message FROM audit.verify_chain(?, ?)`

	var brokenAtIndex sql.NullInt64
	var eventID sql.NullInt64
	var errorMessage sql.NullString

	err := d.queryRow(query, startIndex, endIndex).Scan(&brokenAtIndex, &eventID, &errorMessage)
	if err == sql.ErrNoRows {
		// No rows means chain is valid
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to verify chain: %w", err)
	}

	if brokenAtIndex.Valid {
		return fmt.Errorf("chain broken at index %d (event ID %d): %s",
			brokenAtIndex.Int64, eventID.Int64, errorMessage.String)
	}

	return nil
}

// CleanupOldAuditLogs is DISABLED for append-only audit logs
// Audit logs are immutable and should NEVER be deleted from the database
// For compliance, export old logs to S3 Glacier and keep database entries intact
func (d *Database) CleanupOldAuditLogs(retentionDays int) error {
	return fmt.Errorf("CleanupOldAuditLogs is disabled: audit logs are immutable and must not be deleted")
}

// AuditLogAggregated represents an aggregated audit log entry (for frontend display)
type AuditLogAggregated struct {
	ID                      int       `json:"id"`
	FirstEventID            int64     `json:"first_event_id"`
	LastEventID             int64     `json:"last_event_id"`
	FirstSeen               time.Time `json:"first_seen"`
	LastSeen                time.Time `json:"last_seen"`
	FirstHash               []byte    `json:"first_hash"`
	LastHash                []byte    `json:"last_hash"`
	EventCount              int       `json:"event_count"`
	MinResponseTimeMs       int       `json:"min_response_time_ms"`
	MaxResponseTimeMs       int       `json:"max_response_time_ms"`
	AvgResponseTimeMs       int       `json:"avg_response_time_ms"`
	UserEmail               string    `json:"user_email"`
	UserName                string    `json:"user_name,omitempty"`
	APITokenID              *int64    `json:"api_token_id,omitempty"`
	APITokenName            string    `json:"api_token_name,omitempty"`
	ActionType              string    `json:"action_type"`
	Action                  string    `json:"action"`
	Command                 string    `json:"command,omitempty"`
	Resource                string    `json:"resource,omitempty"`
	ResourceType            string    `json:"resource_type,omitempty"`
	Details                 string    `json:"details,omitempty"`
	IPAddress               string    `json:"ip_address,omitempty"`
	UserAgent               string    `json:"user_agent,omitempty"`
	Status                  string    `json:"status"`
	RBACDecision            string    `json:"rbac_decision,omitempty"`
	HTTPStatus              int       `json:"http_status,omitempty"`
	DeployApprovalRequestID *int64    `json:"deploy_approval_request_id,omitempty"`
}

// GetAuditLogsAggregated retrieves aggregated audit logs with filters (for frontend pagination)
func (d *Database) GetAuditLogsAggregated(filters AuditLogFilters) ([]*AuditLogAggregated, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}

	// Build WHERE clause (use "last_seen" for aggregated view)
	whereClause, args := buildAuditFilterWhereClause(filters, "last_seen")

	// Get total count
	countQuery := "SELECT COUNT(*) FROM \"audit\".\"audit_event_aggregated\" " + whereClause
	var total int
	if err := d.queryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count aggregated audit logs: %w", err)
	}

	// Get paginated results
	query := `
        SELECT "id", "first_event_id", "last_event_id", "first_seen", "last_seen",
               "first_hash", "last_hash", "event_count",
               "min_response_time_ms", "max_response_time_ms", "avg_response_time_ms",
               "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name",
               "action_type", "action", COALESCE("command", ''), COALESCE("resource", ''),
               COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0),
               "deploy_approval_request_id"
        FROM "audit"."audit_event_aggregated" ` + whereClause + `
        ORDER BY "last_seen" DESC
        LIMIT ? OFFSET ?`

	args = append(args, filters.Limit, filters.Offset)

	logs, err := d.queryAggregatedAuditLogs(query, args)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// queryAggregatedAuditLogs executes a query and scans the results into AuditLogAggregated structs
func (d *Database) queryAggregatedAuditLogs(query string, args []interface{}) ([]*AuditLogAggregated, error) {
	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregated audit logs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var logs []*AuditLogAggregated
	for rows.Next() {
		log, err := scanAuditLogAggregated(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan aggregated audit log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// scanAuditLogAggregated scans a single aggregated audit log row
func scanAuditLogAggregated(scanner interface{ Scan(...interface{}) error }) (*AuditLogAggregated, error) {
	log := new(AuditLogAggregated)
	var tokenID sql.NullInt64
	var tokenName sql.NullString
	var deployApprovalRequestID sql.NullInt64

	err := scanner.Scan(
		&log.ID, &log.FirstEventID, &log.LastEventID, &log.FirstSeen, &log.LastSeen,
		&log.FirstHash, &log.LastHash, &log.EventCount,
		&log.MinResponseTimeMs, &log.MaxResponseTimeMs, &log.AvgResponseTimeMs,
		&log.UserEmail, &log.UserName, &tokenID, &tokenName,
		&log.ActionType, &log.Action, &log.Command, &log.Resource,
		&log.ResourceType, &log.Details,
		&log.IPAddress, &log.UserAgent,
		&log.Status, &log.RBACDecision, &log.HTTPStatus,
		&deployApprovalRequestID,
	)
	if err != nil {
		return nil, err
	}

	fields := extractAuditTokenFields(tokenID, tokenName, deployApprovalRequestID)
	applyAuditTokenFieldsToTarget(log, fields)

	return log, nil
}

// GetAuditLogByID retrieves a single audit log by its ID
func (d *Database) GetAuditLogByID(id int64) (*AuditLog, error) {
	query := `
        SELECT
               "id",
               "timestamp",
               "chain_index",
               "previous_hash",
               "event_hash",
               "checkpoint_id",
               "checkpoint_hash",
               "user_email",
               COALESCE("user_name", ''),
               "api_token_id",
               "api_token_name",
               "action_type",
               "action",
               COALESCE("command", ''),
               COALESCE("resource", ''),
               COALESCE("resource_type", ''),
               COALESCE("details", ''),
               COALESCE("request_id", ''),
               COALESCE(host("ip_address"::inet), ''),
               COALESCE("user_agent", ''),
               "status",
               COALESCE("rbac_decision", ''),
               COALESCE("http_status", 0),
               "response_time_ms",
               "event_count",
               "deploy_approval_request_id"
        FROM "audit"."audit_event"
        WHERE id = ?
	`
	row := d.queryRow(query, id)

	log := &AuditLog{}
	var tokenID sql.NullInt64
	var tokenName sql.NullString
	var deployApprovalRequestID sql.NullInt64
	var checkpointID sql.NullString

	err := row.Scan(
		&log.ID, &log.Timestamp, &log.ChainIndex, &log.PreviousHash, &log.EventHash,
		&checkpointID, &log.CheckpointHash,
		&log.UserEmail, &log.UserName, &tokenID, &tokenName,
		&log.ActionType, &log.Action, &log.Command, &log.Resource,
		&log.ResourceType, &log.Details,
		&log.RequestID, &log.IPAddress, &log.UserAgent,
		&log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs, &log.EventCount,
		&deployApprovalRequestID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("audit log %d not found", id)
		}
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	log.CheckpointID = checkpointID.String
	if tokenID.Valid {
		log.APITokenID = &tokenID.Int64
	}
	log.APITokenName = tokenName.String
	if deployApprovalRequestID.Valid {
		log.DeployApprovalRequestID = &deployApprovalRequestID.Int64
	}

	return log, nil
}
