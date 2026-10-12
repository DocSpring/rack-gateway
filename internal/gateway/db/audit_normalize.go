package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"
)

// Column limits of audit.audit_event, in characters (VARCHAR(n) and the CHECKs on command and details).
const (
	auditUserEmailMax    = 254
	auditUserNameMax     = 200
	auditAPITokenNameMax = 150
	auditActionTypeMax   = 100
	auditActionMax       = 150
	auditCommandMax      = 2000
	auditResourceMax     = 255
	auditResourceTypeMax = 100
	auditDetailsMax      = 8000
	auditRequestIDMax    = 255
	auditUserAgentMax    = 512
	auditStatusMax       = 32
	auditRBACDecisionMax = 16
)

// AuditDetailsMaxChars is how many characters of an audit entry's details are kept; longer details are cut.
const AuditDetailsMaxChars = auditDetailsMax

// newAuditChainRow converts an audit log into the exact values its row will hold. Every value is normalized
// before hashing, so the stored row hashes to the stored event_hash, and nothing can make the insert fail: text
// is made valid UTF-8 without NUL bytes and cut to its column's limit (with a visible marker), numbers are
// clamped to INTEGER range, and an IP address that isn't one is stored as NULL.
func newAuditChainRow(log *AuditLog) *AuditChainRow {
	timestamp := log.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return &AuditChainRow{
		HashVersion:             AuditHashVersion,
		Timestamp:               timestamp.UTC().Truncate(time.Microsecond),
		UserEmail:               auditText(log.UserEmail, auditUserEmailMax),
		UserName:                presentAuditText(log.UserName, auditUserNameMax),
		APITokenID:              nullInt64FromPtr(log.APITokenID),
		APITokenName:            optionalAuditText(log.APITokenName, auditAPITokenNameMax),
		ActionType:              auditText(log.ActionType, auditActionTypeMax),
		Action:                  auditText(log.Action, auditActionMax),
		Command:                 presentAuditText(log.Command, auditCommandMax),
		Resource:                presentAuditText(log.Resource, auditResourceMax),
		ResourceType:            presentAuditText(log.ResourceType, auditResourceTypeMax),
		Details:                 presentAuditText(log.Details, auditDetailsMax),
		RequestID:               optionalAuditText(log.RequestID, auditRequestIDMax),
		IPAddress:               canonicalAuditIP(log.IPAddress),
		UserAgent:               presentAuditText(log.UserAgent, auditUserAgentMax),
		Status:                  auditText(log.Status, auditStatusMax),
		RBACDecision:            presentAuditText(log.RBACDecision, auditRBACDecisionMax),
		HTTPStatus:              sql.NullInt32{Int32: clampAuditInt(log.HTTPStatus), Valid: true},
		ResponseTimeMs:          clampAuditInt(log.ResponseTimeMs),
		EventCount:              max(clampAuditInt(log.EventCount), 1),
		DeployApprovalRequestID: nullInt64FromPtr(log.DeployApprovalRequestID),
	}
}

// applyStoredAuditRow copies what was stored back into the caller's audit log.
func applyStoredAuditRow(log *AuditLog, row *AuditChainRow) {
	log.ID = row.ID
	log.Timestamp = row.Timestamp
	log.ChainIndex = row.ChainIndex
	log.PreviousHash = row.PreviousHash
	log.EventHash = row.EventHash
	log.CheckpointID = row.CheckpointID.String
	log.CheckpointHash = row.CheckpointHash
	log.UserEmail = row.UserEmail
	log.UserName = row.UserName.String
	log.APITokenName = row.APITokenName.String
	log.ActionType = row.ActionType
	log.Action = row.Action
	log.Command = row.Command.String
	log.Resource = row.Resource.String
	log.ResourceType = row.ResourceType.String
	log.Details = row.Details.String
	log.RequestID = row.RequestID.String
	log.IPAddress = row.IPAddress.String
	log.UserAgent = row.UserAgent.String
	log.Status = row.Status
	log.RBACDecision = row.RBACDecision.String
	log.HTTPStatus = int(row.HTTPStatus.Int32)
	log.ResponseTimeMs = int(row.ResponseTimeMs)
	log.EventCount = int(row.EventCount)
}

// extractRequestIDFromDetails moves a "request_id" key from the JSON details into the request_id column.
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

// auditText is the stored form of a NOT NULL text column.
func auditText(value string, limit int) string {
	return truncateAuditText(sanitizeAuditText(value), limit)
}

// presentAuditText is the stored form of a nullable column the writer always fills (possibly with "").
func presentAuditText(value string, limit int) sql.NullString {
	return sql.NullString{String: auditText(value, limit), Valid: true}
}

// optionalAuditText is the stored form of a nullable column that holds NULL when the value is blank.
func optionalAuditText(value string, limit int) sql.NullString {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sql.NullString{}
	}
	return presentAuditText(trimmed, limit)
}

// sanitizeAuditText replaces what Postgres can't store in text (invalid UTF-8 and NUL bytes) with U+FFFD.
func sanitizeAuditText(value string) string {
	value = strings.ToValidUTF8(value, string(utf8.RuneError))
	return strings.ReplaceAll(value, "\x00", string(utf8.RuneError))
}

// truncateAuditText cuts value to limit characters. When there's room, the end is replaced with a marker that
// says how long the original was.
func truncateAuditText(value string, limit int) string {
	length := utf8.RuneCountInString(value)
	if length <= limit {
		return value
	}
	marker := fmt.Sprintf("…[truncated from %d characters]", length)
	keep := limit - utf8.RuneCountInString(marker)
	if keep < 0 {
		return string([]rune(value)[:limit])
	}
	return string([]rune(value)[:keep]) + marker
}

// canonicalAuditIP is the stored form of the ip_address (INET) column: the address in Go's canonical text form
// (no zone, which Postgres can't store), or NULL when the value isn't an IP address.
func canonicalAuditIP(value string) sql.NullString {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: addr.WithZone("").String(), Valid: true}
}

// storedAuditIP converts ip_address::text as read back from Postgres ("address/bits") into the form the writer
// hashed. Anything that isn't a single host address is returned unchanged, so it can never match a hash.
func storedAuditIP(value sql.NullString) sql.NullString {
	if !value.Valid {
		return value
	}
	prefix, err := netip.ParsePrefix(value.String)
	if err != nil || prefix.Bits() != prefix.Addr().BitLen() {
		return value
	}
	return sql.NullString{String: prefix.Addr().String(), Valid: true}
}

// clampAuditInt fits a count or status code into a non-negative INTEGER column.
func clampAuditInt(value int) int32 {
	if value < 0 {
		return 0
	}
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(value)
}

func nullInt64FromPtr(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}
