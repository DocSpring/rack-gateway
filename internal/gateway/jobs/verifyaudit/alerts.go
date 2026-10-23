package verifyaudit

import (
	"encoding/json"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// maxAlertedFailures is how many failure messages an alert lists. The summary on the Audit Logs page has them all.
const maxAlertedFailures = 10

// The audit actions of verification alerts. A passing run records neither. A Slack channel gets them when one of
// its action patterns matches; AlertActionPattern matches both, and new Slack integrations route it to their
// #security channel.
var (
	// FailureAction alerts that verification is failing: status failed, or error when the run couldn't verify.
	FailureAction = audit.BuildAction(audit.ActionScopeAuditLog, audit.ActionVerbVerificationFailed)
	// RecoveryAction says verification passes again after failing (status success).
	RecoveryAction = audit.BuildAction(audit.ActionScopeAuditLog, audit.ActionVerbVerificationRecovered)
	// AlertActionPattern matches FailureAction and RecoveryAction.
	AlertActionPattern = audit.BuildAction(audit.ActionScopeAuditLog, "verification_*")
)

// sentryFingerprint groups failing runs into one Sentry issue per result, so runs that keep failing add events
// to it instead of opening an issue every hour.
const sentryFingerprint = "audit-log-verification"

// alertKind is what a run tells people about.
type alertKind int

const (
	noAlert alertKind = iota
	failureAlert
	recoveryAlert
)

// decideAlert alerts on the first failing run, then on a run that finds a failure the last alert didn't report
// or ReminderInterval after it, and once when verification passes again (on each passing run until that alert is
// recorded). Alerting every hour about the same failures would bury a new one; Sentry still gets every failing
// run. After a restart, the previous status says whether verification was failing, so that passing again is
// alerted.
func (s *runState) decideAlert(previous *db.AuditVerificationStatus, o *outcome, now time.Time) alertKind {
	wasFailing := !s.failingSince.IsZero() || !s.recoveryPendingSince.IsZero() ||
		(previous != nil && previous.Result != db.AuditVerificationPassed)
	switch {
	case o.passed() && wasFailing:
		return recoveryAlert
	case o.passed():
		return noAlert
	case s.alertedAt.IsZero(), s.foundNewFailures(o), now.Sub(s.alertedAt) >= ReminderInterval:
		return failureAlert
	default:
		return noAlert
	}
}

// foundNewFailures reports whether the run found failures the streak's latest alert didn't report: more of them,
// or one it didn't list, even when the count is the same (one fixed, another found). Past the failure keys a report
// keeps, any change to the failures counts.
func (s *runState) foundNewFailures(o *outcome) bool {
	if o.failureCount() > s.alertedFailures {
		return true
	}
	if o.report == nil {
		return false
	}
	if !o.report.AllFailureKeys() && o.report.FailureDigest() != s.alertedDigest {
		return true
	}
	for _, key := range o.report.FailureKeys() {
		if !s.alertedKeys[key] {
			return true
		}
	}
	return false
}

// sendAlert records the alert as an audit event, which reaches the Slack channels whose patterns match it. It
// reports whether it was recorded and its notification queued; an alert that wasn't is sent again by the next run.
func (w *Worker) sendAlert(kind alertKind, o *outcome, since time.Time) bool {
	if kind == noAlert {
		return false
	}
	if err := w.audit(alertEntry(kind, o, since)); err != nil {
		gtwlog.Errorf("Audit verification: failed to record the alert: %v", err)
		return false
	}
	return true
}

// alertEntry is the audit event for an alert: a failure (status failed, or error when the run couldn't verify)
// with the first failures, or a recovery (status success).
func alertEntry(kind alertKind, o *outcome, since time.Time) *db.AuditLog {
	action := RecoveryAction
	status := audit.StatusSuccess
	details := map[string]interface{}{
		"result":        o.result(),
		"failing_since": since.UTC().Format(time.RFC3339),
	}
	if kind == failureAlert {
		action = FailureAction
		status = audit.StatusFailed
		details["failure_count"] = o.failureCount()
		if o.err != nil {
			status = audit.StatusError
			details["error"] = o.err.Error()
		}
		// A run that stopped with an error may have found failures first
		if o.failureCount() > 0 {
			details["failures"] = o.report.Failures[:min(len(o.report.Failures), maxAlertedFailures)]
		}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		encoded = []byte("{}")
	}
	return &db.AuditLog{
		UserEmail:  audit.SystemActor,
		UserName:   audit.SystemActor,
		ActionType: audit.ActionTypeSecurity,
		Action:     action,
		Details:    string(encoded),
		Status:     status,
	}
}

// sentryEvent reports a failing run, with its failures and the full summary.
func sentryEvent(o *outcome, since time.Time) *sentry.Event {
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Message = o.headline()
	event.Fingerprint = []string{sentryFingerprint, o.result()}
	event.Tags = map[string]string{"audit_verification": o.result()}
	verification := sentry.Context{
		"failing_since": since.UTC().Format(time.RFC3339),
		"failure_count": o.failureCount(),
		"summary":       o.summary(),
	}
	if o.report != nil {
		verification["failures"] = o.report.Failures
	}
	event.Contexts = map[string]sentry.Context{"audit_verification": verification}
	return event
}
