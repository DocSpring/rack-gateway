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
		// In development/test, use a default secret. Production validation occurs during app init.
		secret = "development-audit-hmac-secret-change-in-production"
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

// CreateAuditLog creates a new audit log entry using cryptographic chain
func (d *Database) CreateAuditLog(log *AuditLog) error {
	if log == nil {
		return fmt.Errorf("audit log cannot be nil")
	}
	normalizeAuditLog(log)
	extractRequestIDFromDetails(log)

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin audit log tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.acquireAuditChainLock(tx); err != nil {
		return err
	}

	latest, err := d.getLatestEvent(tx)
	if err != nil {
		return err
	}

	params := computeChainParams(latest)
	eventHash := computeEventHash(getAuditHMACSecret(), params.chainIndex, params.previousHash, log)

	newID, err := d.appendAuditEvent(tx, log, params, eventHash)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit audit log tx: %w", err)
	}

	populateAuditLogResults(log, newID, params, eventHash)
	return nil
}

func normalizeAuditLog(log *AuditLog) {
	if log.EventCount <= 0 {
		log.EventCount = 1
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
}

func extractRequestIDFromDetails(log *AuditLog) {
	if log.RequestID != "" || log.Details == "" {
		return
	}
	var detailsMap map[string]interface{}
	if err := json.Unmarshal([]byte(log.Details), &detailsMap); err != nil {
		return
	}
	requestID, ok := detailsMap["request_id"].(string)
	if !ok {
		return
	}
	log.RequestID = requestID
	delete(detailsMap, "request_id")
	if updatedDetails, err := json.Marshal(detailsMap); err == nil {
		log.Details = string(updatedDetails)
	}
}

func (d *Database) acquireAuditChainLock(tx *sql.Tx) error {
	query := "SELECT pg_advisory_xact_lock(?)"
	if _, err := d.execTx(tx, query, AdvisoryLockAuditChain); err != nil {
		return fmt.Errorf("failed to acquire audit chain lock: %w", err)
	}
	return nil
}

func (d *Database) getLatestEvent(tx *sql.Tx) (latestEvent, error) {
	var latest latestEvent
	row := tx.QueryRow(d.rebind(`
        SELECT chain_index, event_hash, checkpoint_id, checkpoint_hash
        FROM audit.audit_event
        ORDER BY chain_index DESC
        LIMIT 1
    `))
	err := row.Scan(&latest.ChainIndex, &latest.EventHash, &latest.CheckpointID, &latest.CheckpointHash)
	switch err {
	case sql.ErrNoRows:
		latest.ChainIndex = -1
		return latest, nil
	case nil:
		return latest, nil
	default:
		return latest, fmt.Errorf("failed to get latest event (tx): %w", err)
	}
}

type chainParams struct {
	chainIndex     int64
	previousHash   []byte
	checkpointID   string
	checkpointHash []byte
}

func computeChainParams(latest latestEvent) chainParams {
	if latest.ChainIndex < 0 {
		return chainParams{chainIndex: 0}
	}
	params := chainParams{
		chainIndex:     latest.ChainIndex + 1,
		previousHash:   latest.EventHash,
		checkpointHash: latest.CheckpointHash,
	}
	if latest.CheckpointID.Valid {
		params.checkpointID = latest.CheckpointID.String
	}
	return params
}

func (d *Database) appendAuditEvent(
	tx *sql.Tx,
	log *AuditLog,
	params chainParams,
	eventHash []byte,
) (int64, error) {
	var newID int64
	q := `
        SELECT audit.append_audit_event(
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
            $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
        )
    `
	err := tx.QueryRow(d.rebind(q),
		log.Timestamp,
		params.previousHash,
		eventHash,
		nullableString(params.checkpointID, 255),
		params.checkpointHash,
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
		return 0, fmt.Errorf("failed to append audit log: %w", err)
	}
	return newID, nil
}

func populateAuditLogResults(log *AuditLog, newID int64, params chainParams, eventHash []byte) {
	log.ID = newID
	log.ChainIndex = params.chainIndex
	log.PreviousHash = params.previousHash
	log.EventHash = eventHash
	log.CheckpointID = params.checkpointID
	log.CheckpointHash = params.checkpointHash
}

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
	defer rows.Close() //nolint:errcheck // best-effort close

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
