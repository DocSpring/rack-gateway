package db

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// getAuditHMACSecret retrieves the HMAC secret from environment
func getAuditHMACSecret() []byte {
	secret := os.Getenv("AUDIT_HMAC_SECRET")
	if secret == "" {
		// In development/test, use a default secret
		// In production, this MUST be set to a secure random value
		secret = "development-audit-hmac-secret-change-in-production"
		fmt.Fprintf(os.Stderr, "WARNING: Using default AUDIT_HMAC_SECRET. Set this in production!\n")
	}
	return []byte(secret)
}

// computeEventHash computes the HMAC-SHA256 hash for an audit event
func computeEventHash(secret []byte, chainIndex int64, previousHash []byte, log *AuditLog) []byte {
	h := hmac.New(sha256.New, secret)

	// Write chain index (8 bytes, little-endian)
	indexBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(indexBytes, uint64(chainIndex))
	h.Write(indexBytes)

	// Write previous hash (or empty for genesis)
	if previousHash != nil {
		h.Write(previousHash)
	}

	// Write event data (all fields that should be tamper-evident)
	h.Write([]byte(log.Timestamp.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte(log.UserEmail))
	h.Write([]byte(log.UserName))
	if log.APITokenID != nil {
		tokenIDBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(tokenIDBytes, uint64(*log.APITokenID))
		h.Write(tokenIDBytes)
	}
	h.Write([]byte(log.APITokenName))
	h.Write([]byte(log.ActionType))
	h.Write([]byte(log.Action))
	h.Write([]byte(log.Command))
	h.Write([]byte(log.Resource))
	h.Write([]byte(log.ResourceType))
	h.Write([]byte(log.Details))
	h.Write([]byte(log.IPAddress))
	h.Write([]byte(log.UserAgent))
	h.Write([]byte(log.Status))
	h.Write([]byte(log.RBACDecision))

	httpStatusBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(httpStatusBytes, uint32(log.HTTPStatus))
	h.Write(httpStatusBytes)

	responseTimeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(responseTimeBytes, uint32(log.ResponseTimeMs))
	h.Write(responseTimeBytes)

	if log.DeployApprovalRequestID != nil {
		darIDBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(darIDBytes, uint64(*log.DeployApprovalRequestID))
		h.Write(darIDBytes)
	}

	return h.Sum(nil)
}

// latestEvent represents the latest event in the chain
type latestEvent struct {
	ChainIndex     int64
	EventHash      []byte
	CheckpointID   sql.NullString
	CheckpointHash []byte
}

// getLatestEvent retrieves the latest event from the chain
func (d *Database) getLatestEvent() (*latestEvent, error) {
	row := d.queryRow(`
		SELECT chain_index, event_hash, checkpoint_id, checkpoint_hash
		FROM audit.get_latest_event()
	`)

	var latest latestEvent
	err := row.Scan(&latest.ChainIndex, &latest.EventHash, &latest.CheckpointID, &latest.CheckpointHash)
	if err == sql.ErrNoRows {
		// No events yet (genesis)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest event: %w", err)
	}

	return &latest, nil
}

// CreateAuditLog creates a new audit log entry using cryptographic chain
func (d *Database) CreateAuditLog(log *AuditLog) error {
	if log == nil {
		return fmt.Errorf("audit log cannot be nil")
	}
	if log.EventCount <= 0 {
		log.EventCount = 1
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}

	// Get the latest event in the chain
	latest, err := d.getLatestEvent()
	if err != nil {
		return fmt.Errorf("failed to get latest event: %w", err)
	}

	// Compute chain parameters
	var chainIndex int64
	var previousHash []byte
	var checkpointID string
	var checkpointHash []byte

	if latest == nil {
		// Genesis event
		chainIndex = 0
		previousHash = nil
	} else {
		// Next event in chain
		chainIndex = latest.ChainIndex + 1
		previousHash = latest.EventHash
		if latest.CheckpointID.Valid {
			checkpointID = latest.CheckpointID.String
		}
		checkpointHash = latest.CheckpointHash
	}

	// Extract request_id from details JSON if not already set
	if log.RequestID == "" && log.Details != "" {
		var detailsMap map[string]interface{}
		if err := json.Unmarshal([]byte(log.Details), &detailsMap); err == nil {
			if requestID, ok := detailsMap["request_id"].(string); ok {
				log.RequestID = requestID
				// Remove request_id from details to enable proper aggregation
				delete(detailsMap, "request_id")
				// Re-marshal details without request_id
				if updatedDetails, err := json.Marshal(detailsMap); err == nil {
					log.Details = string(updatedDetails)
				}
			}
		}
	}

	// Compute event hash
	secret := getAuditHMACSecret()
	eventHash := computeEventHash(secret, chainIndex, previousHash, log)

	// Call the append function
	var newID int64
	err = d.queryRow(`
		SELECT audit.append_audit_event(
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
		)
	`,
		log.Timestamp,
		previousHash,
		eventHash,
		nullableString(checkpointID, 255),
		checkpointHash,
		log.UserEmail,
		log.UserName,
		nullableInt64(log.APITokenID),
		nullableString(log.APITokenName, 150),
		log.ActionType,
		log.Action,
		log.Command,
		log.Resource,
		log.ResourceType,
		log.Details,
		nullableString(log.RequestID, 255),
		nullableIP(log.IPAddress),
		log.UserAgent,
		log.Status,
		log.RBACDecision,
		log.HTTPStatus,
		log.ResponseTimeMs,
		log.EventCount,
		nullableInt64(log.DeployApprovalRequestID),
	).Scan(&newID)

	if err != nil {
		return fmt.Errorf("failed to append audit log: %w", err)
	}

	log.ID = newID
	log.ChainIndex = chainIndex
	log.PreviousHash = previousHash
	log.EventHash = eventHash
	log.CheckpointID = checkpointID
	log.CheckpointHash = checkpointHash

	return nil
}

// GetAuditLogs retrieves audit logs with optional filters
func (d *Database) GetAuditLogs(userEmail string, since time.Time, limit int) ([]*AuditLog, error) {
	query := `
        SELECT "id", "timestamp", "chain_index", "previous_hash", "event_hash", "checkpoint_id", "checkpoint_hash",
               "user_email", COALESCE("user_name", ''), "api_token_id", "api_token_name", "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE("request_id", ''), COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms",
               "event_count", "deploy_approval_request_id"
        FROM "audit"."audit_event"
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
		log, err := scanAuditLog(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// scanAuditLog scans a single audit log row
func scanAuditLog(scanner interface{ Scan(...interface{}) error }) (*AuditLog, error) {
	log := new(AuditLog)
	var tokenID sql.NullInt64
	var tokenName sql.NullString
	var deployApprovalRequestID sql.NullInt64
	var checkpointID sql.NullString
	var previousHash []byte
	var checkpointHash []byte

	err := scanner.Scan(
		&log.ID, &log.Timestamp, &log.ChainIndex, &previousHash, &log.EventHash, &checkpointID, &checkpointHash,
		&log.UserEmail, &log.UserName,
		&tokenID, &tokenName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details,
		&log.RequestID, &log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs, &log.EventCount,
		&deployApprovalRequestID,
	)
	if err != nil {
		return nil, err
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
	if checkpointID.Valid {
		log.CheckpointID = checkpointID.String
	}
	log.PreviousHash = previousHash
	log.CheckpointHash = checkpointHash

	return log, nil
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

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var logs []*AuditLog
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
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
	query := `SELECT broken_at_index, event_id, error_message FROM audit.verify_chain($1, $2)`

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

	// Build WHERE clause
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
		whereClause += " AND \"last_seen\" >= ?"
		args = append(args, filters.Since.UTC())
	}
	if !filters.Until.IsZero() {
		whereClause += " AND \"last_seen\" <= ?"
		args = append(args, filters.Until.UTC())
	}

	// Full-text search
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

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query aggregated audit logs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close

	var logs []*AuditLogAggregated
	for rows.Next() {
		log := new(AuditLogAggregated)
		var tokenID sql.NullInt64
		var tokenName sql.NullString
		var deployApprovalRequestID sql.NullInt64

		err := rows.Scan(
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
			return nil, 0, fmt.Errorf("failed to scan aggregated audit log: %w", err)
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
