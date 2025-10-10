package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// CreateAuditLog creates a new audit log entry
func (d *Database) CreateAuditLog(log *AuditLog) error {
	if log == nil {
		return fmt.Errorf("audit log cannot be nil")
	}
	if log.EventCount <= 0 {
		log.EventCount = 1
	}

	if shouldAggregateAudit(log.Action) {
		updated, err := d.tryIncrementAuditLog(log)
		if err != nil {
			return err
		}
		if updated {
			return nil
		}
	}

	_, err := d.exec(
		`INSERT INTO audit_logs (
            user_email, user_name, api_token_id, api_token_name, action_type, action, command, resource, resource_type,
            details, ip_address, user_agent, status, rbac_decision, http_status, response_time_ms, event_count, deploy_approval_request_id
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::inet, ?, ?, ?, ?, ?, ?, ?)`,
		log.UserEmail, log.UserName, nullableInt64(log.APITokenID), nullableString(log.APITokenName, 150),
		log.ActionType, log.Action, log.Command, log.Resource, log.ResourceType,
		log.Details, nullableIP(log.IPAddress), log.UserAgent, log.Status, log.RBACDecision, log.HTTPStatus, log.ResponseTimeMs, log.EventCount,
		nullableInt64(log.DeployApprovalRequestID),
	)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}
	return nil
}

func shouldAggregateAudit(action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	if strings.HasSuffix(action, ".read") {
		return true
	}
	if strings.HasSuffix(action, ".list") {
		return true
	}
	return false
}

type auditLogSnapshot struct {
	ID           int64
	APITokenID   *int64
	APITokenName string
	ActionType   string
	Action       string
	Command      string
	Resource     string
	ResourceType string
	Details      string
	IPAddress    string
	UserAgent    string
	Status       string
	RBACDecision string
	HTTPStatus   int
	ResponseTime int
	EventCount   int
}

func (d *Database) tryIncrementAuditLog(log *AuditLog) (bool, error) {
	row := d.queryRow(
		`SELECT "id", "api_token_id", "api_token_name", "action_type", "action", COALESCE("command", ''), COALESCE("resource", ''),
		        COALESCE("resource_type", ''), COALESCE("details", ''), COALESCE(host("ip_address"::inet), ''),
		        COALESCE("user_agent", ''), "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0),
		        "response_time_ms", "event_count"
		 FROM "audit_logs"
		 WHERE "user_email" = ?
		   AND "timestamp" >= NOW() - INTERVAL '10 seconds'
		 ORDER BY "timestamp" DESC
		 LIMIT 1`, log.UserEmail,
	)
	var prev auditLogSnapshot
	var prevTokenID sql.NullInt64
	var prevTokenName sql.NullString
	if err := row.Scan(
		&prev.ID, &prevTokenID, &prevTokenName, &prev.ActionType, &prev.Action, &prev.Command, &prev.Resource, &prev.ResourceType,
		&prev.Details, &prev.IPAddress, &prev.UserAgent, &prev.Status, &prev.RBACDecision,
		&prev.HTTPStatus, &prev.ResponseTime, &prev.EventCount,
	); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to fetch previous audit log: %w", err)
	}
	if prevTokenID.Valid {
		id := prevTokenID.Int64
		prev.APITokenID = &id
	}
	if prevTokenName.Valid {
		prev.APITokenName = prevTokenName.String
	}

	if !shouldAggregateAudit(prev.Action) {
		return false, nil
	}

	if !strings.EqualFold(strings.TrimSpace(prev.ActionType), strings.TrimSpace(log.ActionType)) {
		return false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(prev.Action), strings.TrimSpace(log.Action)) {
		return false, nil
	}
	if strings.TrimSpace(prev.Command) != strings.TrimSpace(log.Command) {
		return false, nil
	}
	if strings.TrimSpace(prev.Resource) != strings.TrimSpace(log.Resource) {
		return false, nil
	}
	if strings.TrimSpace(prev.ResourceType) != strings.TrimSpace(log.ResourceType) {
		return false, nil
	}
	if strings.TrimSpace(prev.IPAddress) != strings.TrimSpace(log.IPAddress) {
		return false, nil
	}
	if strings.TrimSpace(prev.UserAgent) != strings.TrimSpace(log.UserAgent) {
		return false, nil
	}
	if !equalInt64Ptr(prev.APITokenID, log.APITokenID) {
		return false, nil
	}
	if strings.TrimSpace(prev.APITokenName) != strings.TrimSpace(log.APITokenName) {
		return false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(prev.Status), strings.TrimSpace(log.Status)) {
		return false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(prev.RBACDecision), strings.TrimSpace(log.RBACDecision)) {
		return false, nil
	}
	if prev.HTTPStatus != log.HTTPStatus {
		return false, nil
	}
	if !detailsEquivalent(prev.Details, log.Details) {
		return false, nil
	}

	_, err := d.exec(
		`UPDATE audit_logs
		 SET timestamp = NOW(), details = ?, command = ?, status = ?, rbac_decision = ?,
		     http_status = ?, response_time_ms = ?, event_count = event_count + 1,
		     api_token_id = COALESCE(?, api_token_id), api_token_name = COALESCE(?, api_token_name)
		 WHERE id = ?`,
		log.Details, log.Command, log.Status, log.RBACDecision, log.HTTPStatus, log.ResponseTimeMs,
		nullableInt64(log.APITokenID), nullableString(log.APITokenName, 150), prev.ID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to increment audit log: %w", err)
	}
	log.EventCount = prev.EventCount + 1
	return true, nil
}

func equalInt64Ptr(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func detailsEquivalent(a, b string) bool {
	na := normalizeDetails(a)
	nb := normalizeDetails(b)
	return reflect.DeepEqual(na, nb)
}

func normalizeDetails(details string) interface{} {
	trimmed := strings.TrimSpace(details)
	if trimmed == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		delete(obj, "request_id")
		if len(obj) == 0 {
			return nil
		}
		return obj
	}
	return trimmed
}

// GetAuditLogs retrieves audit logs with optional filters
func (d *Database) GetAuditLogs(userEmail string, since time.Time, limit int) ([]*AuditLog, error) {
	query := `
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name", "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''), "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms", "event_count",
               "deploy_approval_request_id"
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
	defer rows.Close() //nolint:errcheck // best-effort close

	var logs []*AuditLog
	for rows.Next() {
		log := new(AuditLog)
		var tokenID sql.NullInt64
		var tokenName sql.NullString
		var deployApprovalRequestID sql.NullInt64

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName,
			&tokenID, &tokenName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details,
			&log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs, &log.EventCount,
			&deployApprovalRequestID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		if tokenID.Valid {
			id := tokenID.Int64
			log.APITokenID = &id
		}
		if tokenName.Valid {
			log.APITokenName = tokenName.String
		}
		if deployApprovalRequestID.Valid {
			id := deployApprovalRequestID.Int64
			log.DeployApprovalRequestID = &id
		}

		logs = append(logs, log)
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
	Range        string
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
            "api_token_name" ILIKE ? OR
            "action" ILIKE ? OR
            "resource" ILIKE ? OR
            "details" ILIKE ? OR
            host("ip_address"::inet) ILIKE ? OR
            "user_agent" ILIKE ?
        )`
		searchPattern := "%" + filters.Search + "%"
		for i := 0; i < 8; i++ {
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
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name", "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''),
               COALESCE("details", ''), COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms", "event_count", "deploy_approval_request_id"
        FROM "audit_logs" ` + whereClause + `
        ORDER BY "timestamp" DESC
		LIMIT ? OFFSET ?`

	args = append(args, filters.Limit, filters.Offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var logs []*AuditLog
	for rows.Next() {
		log := new(AuditLog)
		var tokenID sql.NullInt64
		var tokenName sql.NullString
		var deployApprovalRequestID sql.NullInt64
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName, &tokenID, &tokenName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details, &log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs, &log.EventCount, &deployApprovalRequestID); err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
		}
		if tokenID.Valid {
			id := tokenID.Int64
			log.APITokenID = &id
		}
		if tokenName.Valid {
			log.APITokenName = tokenName.String
		}
		if deployApprovalRequestID.Valid {
			id := deployApprovalRequestID.Int64
			log.DeployApprovalRequestID = &id
		}
		logs = append(logs, log)
	}
	return logs, total, nil
}

// GetAuditLogsByDeployApprovalRequestID retrieves audit logs for a specific deploy approval request
func (d *Database) GetAuditLogsByDeployApprovalRequestID(deployApprovalRequestID int64, limit int) ([]*AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name", "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''), "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms", "event_count",
               "deploy_approval_request_id"
        FROM "audit_logs"
        WHERE "deploy_approval_request_id" = ?
        ORDER BY "timestamp" DESC
        LIMIT ?
    `

	rows, err := d.query(query, deployApprovalRequestID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs by deploy approval request ID: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var logs []*AuditLog
	for rows.Next() {
		log := new(AuditLog)
		var tokenID sql.NullInt64
		var tokenName sql.NullString
		var deployApprovalReqID sql.NullInt64

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName,
			&tokenID, &tokenName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details,
			&log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs, &log.EventCount,
			&deployApprovalReqID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		if tokenID.Valid {
			id := tokenID.Int64
			log.APITokenID = &id
		}
		if tokenName.Valid {
			log.APITokenName = tokenName.String
		}
		if deployApprovalReqID.Valid {
			id := deployApprovalReqID.Int64
			log.DeployApprovalRequestID = &id
		}

		logs = append(logs, log)
	}

	return logs, nil
}

// CleanupOldAuditLogs deletes audit logs older than retentionDays
func (d *Database) CleanupOldAuditLogs(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	_, err := d.exec("DELETE FROM audit_logs WHERE timestamp < NOW() - (INTERVAL '1 day' * ?::int)", retentionDays)
	return err
}
