package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// ChainWriter appends entries to the audit hash chain. *db.Database is the real one.
type ChainWriter interface {
	CreateAuditLog(log *db.AuditLog) error
}

// SetChainWriter replaces where the logger appends entries (by default, the database it was created with). Tests
// use a writer that always fails to see how requests behave during an audit log outage.
func (l *Logger) SetChainWriter(writer ChainWriter) {
	l.writer = writer
}

// write appends al to the hash chain (only to stdout for a logger without a database). A failure is logged and
// sent to Sentry before it's returned: either the request is about to be refused (an attempt), or it has already
// acted and its outcome has no record.
func (l *Logger) write(al *db.AuditLog) error {
	if l.writer == nil {
		writeEntryLine(al)
		return nil
	}
	if err := appendEntry(l.writer, al); err != nil {
		reportWriteFailure(al, err)
		return err
	}
	return nil
}

// appendEntry mirrors al to stdout for CloudWatch, then appends it to the hash chain. The outcome feeds the health
// check (WriteHealth).
func appendEntry(writer ChainWriter, al *db.AuditLog) error {
	writeEntryLine(al)
	if al.Timestamp.IsZero() {
		al.Timestamp = time.Now().UTC()
	}
	err := writer.CreateAuditLog(al)
	noteWriteResult(err, time.Now())
	return err
}

// RecordedInTransaction handles the audit events a database operation appended in its own transaction, so the
// change and its record committed together (or neither did): the events are mirrored to stdout like every other
// entry, and a failed append (an error wrapping db.ErrAuditAppend) is logged, sent to Sentry and fails the health
// check.
func RecordedInTransaction(events []*db.AuditLog, err error) {
	if errors.Is(err, db.ErrAuditAppend) {
		noteWriteResult(err, time.Now())
		gtwlog.Errorf("audit: %v", err)
		sentry.CaptureException(err)
		return
	}
	if err != nil || len(events) == 0 {
		return
	}
	for _, event := range events {
		writeEntryLine(event)
	}
	noteWriteResult(nil, time.Now())
}

// writeEntryLine writes al to stdout as a structured line for CloudWatch. Verbose request details are left out:
// the method and path are logged with the request.
func writeEntryLine(al *db.AuditLog) {
	payload := map[string]interface{}{
		"ts":            time.Now().UTC().Format(time.RFC3339),
		"user_email":    al.UserEmail,
		"user_name":     al.UserName,
		"action_type":   al.ActionType,
		"action":        al.Action,
		"resource":      al.Resource,
		"resource_type": al.ResourceType,
		"command":       al.Command,
		"status":        al.Status,
		"rbac_decision": al.RBACDecision,
		"http_status":   al.HTTPStatus,
		"latency_ms":    al.ResponseTimeMs,
		"ip_address":    al.IPAddress,
		"user_agent":    al.UserAgent,
		"event_count":   max(al.EventCount, 1),
	}
	if al.APITokenID != nil {
		payload["api_token_id"] = *al.APITokenID
	}
	if name := strings.TrimSpace(al.APITokenName); name != "" {
		payload["api_token_name"] = name
	}
	if al.RequestID != "" {
		payload["request_id"] = al.RequestID
	}
	data, err := json.Marshal(payload)
	if err != nil {
		gtwlog.Errorf("audit: failed to marshal audit db log: %v", err)
		return
	}
	WriteAuditLine(data)
}

// reportWriteFailure logs an entry that couldn't be written and sends the failure to Sentry.
func reportWriteFailure(al *db.AuditLog, err error) {
	failure := fmt.Errorf("audit: failed to write %s entry (%s) for %s: %w", al.Action, al.Status, actor(al), err)
	gtwlog.Errorf("%v", failure)
	sentry.CaptureException(failure)
}

// actor names who an entry is about: the API token when there is one, else the user.
func actor(al *db.AuditLog) string {
	if al.APITokenID != nil {
		return fmt.Sprintf("api token %d (%s)", *al.APITokenID, al.UserEmail)
	}
	return al.UserEmail
}
