package db

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Hash versions stored in audit.audit_event.hash_version.
const (
	// AuditHashVersionLegacy marks events written before hash v2. Their HMAC can't be recomputed from the stored
	// row (see docs), so verification only checks their previous_hash links.
	AuditHashVersionLegacy int16 = 1
	// AuditHashVersion is the version every new event is written with.
	AuditHashVersion int16 = 2
)

// devAuditHMACSecret is only used to write events in development and tests, where AUDIT_HMAC_SECRET is often
// unset. The server refuses to start outside DEV_MODE without a real secret, and verification never uses it.
const devAuditHMACSecret = "development-audit-hmac-secret-change-in-production"

// auditHashV2Prefix starts every v2 hash input, so it can't be mistaken for any other encoding.
const auditHashV2Prefix = "\x02rack-gateway.audit_event"

// auditNullMarker encodes SQL NULL. A present value always starts with its length (a digit), so the two can't
// be confused.
const auditNullMarker = '~'

// AuditChainRow is an audit event exactly as stored in audit.audit_event. The writer builds it from an AuditLog
// (normalized to what the columns hold), hashes it, then stores it; verification reads it back and hashes it
// again. Nullable columns use sql.Null* types so NULL and "" hash differently.
type AuditChainRow struct {
	ID                      int64
	ChainIndex              int64
	HashVersion             int16
	Timestamp               time.Time // microsecond precision, UTC
	PreviousHash            []byte    // nil (NULL) only for the genesis event
	EventHash               []byte
	CheckpointID            sql.NullString
	CheckpointHash          []byte // nil is NULL
	UserEmail               string
	UserName                sql.NullString
	APITokenID              sql.NullInt64
	APITokenName            sql.NullString
	ActionType              string
	Action                  string
	Command                 sql.NullString
	Resource                sql.NullString
	ResourceType            sql.NullString
	Details                 sql.NullString
	RequestID               sql.NullString
	IPAddress               sql.NullString // canonical address text, see canonicalAuditIP
	UserAgent               sql.NullString
	Status                  string
	RBACDecision            sql.NullString
	HTTPStatus              sql.NullInt32
	ResponseTimeMs          int32
	EventCount              int32
	DeployApprovalRequestID sql.NullInt64
}

// AuditHMACSecretFromEnv returns AUDIT_HMAC_SECRET. Verification uses it and must never fall back to the
// development secret, so a missing secret is an error.
func AuditHMACSecretFromEnv() ([]byte, error) {
	secret := os.Getenv("AUDIT_HMAC_SECRET")
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("AUDIT_HMAC_SECRET is not set: it is required to verify audit event hashes")
	}
	return []byte(secret), nil
}

// auditWriterSecret is the secret used to write events: AUDIT_HMAC_SECRET, or the development secret when it
// isn't set (the server checks for a real secret at startup outside DEV_MODE).
func auditWriterSecret() []byte {
	if secret := os.Getenv("AUDIT_HMAC_SECRET"); secret != "" {
		return []byte(secret)
	}
	return []byte(devAuditHMACSecret)
}

// ComputeAuditEventHash returns the HMAC-SHA256 of a hash v2 event: every stored column except event_hash, in a
// fixed order, each length-prefixed (or marked NULL) so content can't move between fields without changing the
// hash.
func ComputeAuditEventHash(secret []byte, row *AuditChainRow) ([]byte, error) {
	if len(secret) == 0 {
		return nil, errors.New("audit HMAC secret is empty")
	}
	if row == nil {
		return nil, errors.New("audit event is nil")
	}
	if row.HashVersion != AuditHashVersion {
		return nil, fmt.Errorf("cannot compute hash version %d (only version %d)", row.HashVersion, AuditHashVersion)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(encodeAuditEventV2(row))
	return mac.Sum(nil), nil
}

func encodeAuditEventV2(row *AuditChainRow) []byte {
	e := auditHashEncoder{buf: make([]byte, 0, 512)}
	e.buf = append(e.buf, auditHashV2Prefix...)
	e.int(row.ID)
	e.int(row.ChainIndex)
	e.optionalBytes(row.PreviousHash)
	e.int(row.Timestamp.UnixMicro())
	e.nullString(row.CheckpointID)
	e.optionalBytes(row.CheckpointHash)
	e.str(row.UserEmail)
	e.nullString(row.UserName)
	e.nullInt64(row.APITokenID)
	e.nullString(row.APITokenName)
	e.str(row.ActionType)
	e.str(row.Action)
	e.nullString(row.Command)
	e.nullString(row.Resource)
	e.nullString(row.ResourceType)
	e.nullString(row.Details)
	e.nullString(row.RequestID)
	e.nullString(row.IPAddress)
	e.nullString(row.UserAgent)
	e.str(row.Status)
	e.nullString(row.RBACDecision)
	e.nullInt32(row.HTTPStatus)
	e.int(int64(row.ResponseTimeMs))
	e.int(int64(row.EventCount))
	e.nullInt64(row.DeployApprovalRequestID)
	return e.buf
}

// auditHashEncoder writes each field as a netstring ("<length>:<bytes>,") or as auditNullMarker for NULL.
// Integers are written in decimal.
type auditHashEncoder struct {
	buf []byte
}

func (e *auditHashEncoder) str(value string) {
	e.buf = strconv.AppendInt(e.buf, int64(len(value)), 10)
	e.buf = append(e.buf, ':')
	e.buf = append(e.buf, value...)
	e.buf = append(e.buf, ',')
}

func (e *auditHashEncoder) optionalBytes(value []byte) {
	if value == nil {
		e.buf = append(e.buf, auditNullMarker)
		return
	}
	e.str(string(value))
}

func (e *auditHashEncoder) int(value int64) {
	e.str(strconv.FormatInt(value, 10))
}

func (e *auditHashEncoder) nullString(value sql.NullString) {
	if !value.Valid {
		e.buf = append(e.buf, auditNullMarker)
		return
	}
	e.str(value.String)
}

func (e *auditHashEncoder) nullInt64(value sql.NullInt64) {
	if !value.Valid {
		e.buf = append(e.buf, auditNullMarker)
		return
	}
	e.int(value.Int64)
}

func (e *auditHashEncoder) nullInt32(value sql.NullInt32) {
	if !value.Valid {
		e.buf = append(e.buf, auditNullMarker)
		return
	}
	e.int(int64(value.Int32))
}
