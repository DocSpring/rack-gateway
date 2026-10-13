package db

import (
	"database/sql"
	"fmt"
	"time"
)

// GetAuditLogs retrieves audit logs with optional filters
func (d *Database) GetAuditLogs(
	userEmail string,
	since time.Time,
	limit int,
) ([]*AuditLog, error) {
	query := `
        SELECT "id", "timestamp", "chain_index", "previous_hash", "event_hash",
               "checkpoint_id", "checkpoint_hash", "user_email",
               COALESCE("user_name", ''), "api_token_id", "api_token_name",
               "action_type", "action", COALESCE("command", ''),
               COALESCE("resource", ''), COALESCE("resource_type", ''),
               COALESCE("details", ''), COALESCE("request_id", ''),
               COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''),
               COALESCE("http_status", 0), "response_time_ms",
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
	defer func() { _ = rows.Close() }()

	return scanAuditLogs(rows)
}

type auditTokenScanState struct {
	tokenID   sql.NullInt64
	tokenName sql.NullString
}

func newAuditTokenScanState() *auditTokenScanState {
	return &auditTokenScanState{}
}

func (s *auditTokenScanState) destinations() []interface{} {
	return []interface{}{&s.tokenID, &s.tokenName}
}

func (s *auditTokenScanState) apply(target interface{}, deployID sql.NullInt64) {
	fields := extractAuditTokenFields(s.tokenID, s.tokenName, deployID)
	applyAuditTokenFieldsToTarget(target, fields)
}

func (s *auditTokenScanState) appendArgs(prefix []interface{}, suffix ...interface{}) []interface{} {
	capacity := len(prefix) + len(s.destinations()) + len(suffix)
	args := make([]interface{}, 0, capacity)
	args = append(args, prefix...)
	args = append(args, s.destinations()...)
	return append(args, suffix...)
}

func scanAuditRow(
	scanner interface{ Scan(...interface{}) error },
	args []interface{},
	tokenState *auditTokenScanState,
	target interface{},
	deployID *sql.NullInt64,
) error {
	if err := scanner.Scan(args...); err != nil {
		return err
	}

	tokenState.apply(target, *deployID)
	return nil
}

// buildAuditCommonFields creates the common field pointers used by both scanAuditLog and scanAuditLogAggregated
func buildAuditCommonFields(log interface{}) []interface{} {
	switch v := log.(type) {
	case *AuditLog:
		return []interface{}{&v.ActionType, &v.Action, &v.Command, &v.Resource, &v.ResourceType, &v.Details}
	case *AuditLogAggregated:
		return []interface{}{&v.ActionType, &v.Action, &v.Command, &v.Resource, &v.ResourceType, &v.Details}
	default:
		return nil
	}
}

// scanAuditLog scans a single audit log row
func scanAuditLog(scanner interface{ Scan(...interface{}) error }) (*AuditLog, error) {
	log := new(AuditLog)
	tokenState := newAuditTokenScanState()
	var deployApprovalRequestID sql.NullInt64
	var checkpointID sql.NullString
	var previousHash []byte
	var checkpointHash []byte

	prefix := []interface{}{
		&log.ID,
		&log.Timestamp,
		&log.ChainIndex,
		&previousHash,
		&log.EventHash,
		&checkpointID,
		&checkpointHash,
		&log.UserEmail,
		&log.UserName,
	}
	args := tokenState.appendArgs(prefix, buildAuditCommonFields(log)...)
	args = append(args,
		&log.RequestID, &log.IPAddress, &log.UserAgent,
		&log.Status, &log.RBACDecision, &log.HTTPStatus,
		&log.ResponseTimeMs, &log.EventCount, &deployApprovalRequestID,
	)

	if err := scanAuditRow(scanner, args, tokenState, log, &deployApprovalRequestID); err != nil {
		return nil, err
	}

	if checkpointID.Valid {
		log.CheckpointID = checkpointID.String
	}
	log.PreviousHash = previousHash
	log.CheckpointHash = checkpointHash

	return log, nil
}

func optionalInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func stringFromNull(value sql.NullString) (string, bool) {
	if !value.Valid {
		return "", false
	}
	return value.String, true
}

type auditTokenFieldSet struct {
	tokenID  *int64
	name     string
	hasName  bool
	deployID *int64
}

func extractAuditTokenFields(
	tokenID sql.NullInt64,
	tokenName sql.NullString,
	deployID sql.NullInt64,
) auditTokenFieldSet {
	fields := auditTokenFieldSet{}
	if id := optionalInt64Ptr(tokenID); id != nil {
		fields.tokenID = id
	}
	if name, ok := stringFromNull(tokenName); ok {
		fields.name = name
		fields.hasName = true
	}
	if id := optionalInt64Ptr(deployID); id != nil {
		fields.deployID = id
	}
	return fields
}

func applyAuditTokenFieldsToTarget(target interface{}, fields auditTokenFieldSet) {
	if fields.tokenID != nil {
		switch t := target.(type) {
		case *AuditLog:
			t.APITokenID = fields.tokenID
		case *AuditLogAggregated:
			t.APITokenID = fields.tokenID
		}
	}
	if fields.hasName {
		switch t := target.(type) {
		case *AuditLog:
			t.APITokenName = fields.name
		case *AuditLogAggregated:
			t.APITokenName = fields.name
		}
	}
	if fields.deployID != nil {
		switch t := target.(type) {
		case *AuditLog:
			t.DeployApprovalRequestID = fields.deployID
		case *AuditLogAggregated:
			t.DeployApprovalRequestID = fields.deployID
		}
	}
}
