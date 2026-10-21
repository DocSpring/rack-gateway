package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// StatusAttempted is the status of the entry a fail-closed request writes before it acts (see rbac.AuditPolicy).
// The entries written once it has acted are its results.
const StatusAttempted = "attempted"

// ErrAuditUnavailable means an attempt entry couldn't be written, so its request must be refused without acting.
var ErrAuditUnavailable = errors.New("audit log unavailable")

// errNoLogger is the failure reported for an attempt made without an audit logger.
var errNoLogger = errors.New("no audit logger")

// Attempt is the entry a fail-closed request wrote before acting. Every entry written for the request afterwards
// with LogRequestEntry (its results) carries the attempt's request ID, which the gateway generates for each
// request, so the attempt and its results can be paired.
type Attempt struct {
	// ID is the attempt entry's audit event ID.
	ID int64
	// RequestID is the request's ID, shared by the attempt and its results.
	RequestID string
	results   atomic.Int32
}

type attemptContextKey struct{}

// AttemptFromContext returns the attempt recorded for the request whose context is ctx, or nil.
func AttemptFromContext(ctx context.Context) *Attempt {
	attempt, _ := ctx.Value(attemptContextKey{}).(*Attempt)
	return attempt
}

// Results returns how many result entries have been written for the attempt's request.
func (a *Attempt) Results() int {
	return int(a.results.Load())
}

// RefusalMessage is the error a request gets when it's refused because its attempt entry couldn't be written.
func RefusalMessage(action string) string {
	return fmt.Sprintf("audit log unavailable; refusing to perform %s", action)
}

// RecordAttempt writes entry as the attempt of request r, before r acts, and returns r's context carrying the
// Attempt. When the entry can't be written (or there's no logger to write it), the failure is reported and the
// returned error wraps ErrAuditUnavailable: the caller must refuse the request (503) without acting.
//
// Attempts aren't sent to Slack. The result entries are, so a channel hears about each action once.
func (l *Logger) RecordAttempt(r *http.Request, entry *db.AuditLog) (context.Context, error) {
	if l == nil {
		reportWriteFailure(entry, errNoLogger)
		return r.Context(), ErrAuditUnavailable
	}
	requestID := RequestID(r)
	entry.Status = StatusAttempted
	entry.RBACDecision = "allow"
	setRequestID(entry, requestID)
	if err := l.write(entry); err != nil {
		return r.Context(), fmt.Errorf("%w: %w", ErrAuditUnavailable, err)
	}
	attempt := &Attempt{ID: entry.ID, RequestID: requestID}
	return context.WithValue(r.Context(), attemptContextKey{}, attempt), nil
}

// RecordSystemAttempt writes entry as the attempt of something the gateway does by itself (a background job acting
// on an outside service), before it acts. The entry gets a new request ID unless it has one; give the result entry
// the same RequestID so the two are paired. Like RecordAttempt, the attempt isn't sent to Slack, and when it can't
// be written the returned error wraps ErrAuditUnavailable and the caller must not act.
func (l *Logger) RecordSystemAttempt(entry *db.AuditLog) error {
	if l == nil {
		reportWriteFailure(entry, errNoLogger)
		return ErrAuditUnavailable
	}
	entry.Status = StatusAttempted
	entry.RBACDecision = "allow"
	if entry.RequestID == "" {
		entry.RequestID = uuid.NewString()
	}
	if err := l.write(entry); err != nil {
		return fmt.Errorf("%w: %w", ErrAuditUnavailable, err)
	}
	return nil
}

// LogRequestEntry writes an entry for the request whose context is ctx. When the request recorded an attempt,
// the entry is one of its results and gets its request ID. Like LogDBEntry, a failure is reported and returned;
// the request has already acted, so there's nothing more a caller can do about it.
func (l *Logger) LogRequestEntry(ctx context.Context, al *db.AuditLog) error {
	attempt := AttemptFromContext(ctx)
	if attempt == nil {
		return l.LogDBEntry(al)
	}
	setRequestID(al, attempt.RequestID)
	if err := l.LogDBEntry(al); err != nil {
		gtwlog.Errorf("audit: request %s has acted but its %s result wasn't recorded; attempt entry %d is its record",
			attempt.RequestID, al.Action, attempt.ID)
		return err
	}
	attempt.results.Add(1)
	return nil
}

// setRequestID gives al the request ID, unless it already has one, or its details carry one (proxied requests'
// details do; the database moves it into the request_id column when it stores the entry).
func setRequestID(al *db.AuditLog, requestID string) {
	if al.RequestID != "" || detailsCarryRequestID(al.Details) {
		return
	}
	al.RequestID = requestID
}

func detailsCarryRequestID(details string) bool {
	if details == "" {
		return false
	}
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(details), &fields); err != nil {
		return false
	}
	_, ok := fields["request_id"].(string)
	return ok
}
