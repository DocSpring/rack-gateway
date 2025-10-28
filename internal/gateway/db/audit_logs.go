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

	return scanAuditLogs(rows)
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

	fields := extractAuditTokenFields(tokenID, tokenName, deployApprovalRequestID)
	applyAuditTokenFieldsToTarget(log, fields)
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

func extractAuditTokenFields(tokenID sql.NullInt64, tokenName sql.NullString, deployID sql.NullInt64) auditTokenFieldSet {
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
