package verifyaudit

import (
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// runPlan is what a run checks.
type runPlan struct {
	full bool
	// from is the first chain index the run checks (0 for a full run).
	from int64
	// verified is the latest event a passing run checked: the chain must still hold it.
	verified *auditverify.VerifiedEvent
}

// outcome is what a run found.
type outcome struct {
	plan runPlan
	// anchors is whether anchors are configured (and so checked).
	anchors bool
	// report is nil when the run couldn't verify, and err says why. A run that stopped with an error partway has
	// both: the report holds what it found before it stopped.
	report *auditverify.Report
	err    error
}

func (o *outcome) passed() bool {
	return o.err == nil && o.report.OK()
}

func (o *outcome) result() string {
	switch {
	case o.err != nil:
		return db.AuditVerificationError
	case !o.report.OK():
		return db.AuditVerificationFailed
	default:
		return db.AuditVerificationPassed
	}
}

func (o *outcome) failureCount() int {
	if o.report == nil {
		return 0
	}
	return o.report.FailureCount
}

func (o *outcome) warningCount() int {
	if o.report == nil {
		return 0
	}
	return o.report.WarningCount
}

// headline describes the outcome in a line.
func (o *outcome) headline() string {
	switch o.result() {
	case db.AuditVerificationError:
		if o.failureCount() > 0 {
			return fmt.Sprintf("Audit log verification couldn't finish: %v. It had found %d integrity failures, "+
				"first: %s", o.err, o.report.FailureCount, o.report.Failures[0])
		}
		return fmt.Sprintf("Audit log verification couldn't run: %v", o.err)
	case db.AuditVerificationFailed:
		return fmt.Sprintf("Audit log verification failed: %d integrity failures, first: %s",
			o.report.FailureCount, o.report.Failures[0])
	default:
		return fmt.Sprintf("Audit log verification passed: %d events checked, %d anchors, %d warnings",
			o.report.EventsChecked, o.report.AnchorsChecked, o.report.WarningCount)
	}
}

// summary is the report as verify-audit prints it, after a line saying what the run checked.
func (o *outcome) summary() string {
	var summary strings.Builder
	summary.WriteString(o.scope() + "\n")
	if o.err != nil {
		summary.WriteString(o.headline() + "\n")
		if o.report == nil {
			return summary.String()
		}
	}
	if err := o.report.Write(&summary); err != nil {
		summary.WriteString(err.Error() + "\n")
	}
	return summary.String()
}

// scope says what the run checks.
func (o *outcome) scope() string {
	scope := "Checking every event"
	if !o.plan.full {
		scope = fmt.Sprintf("Checking the events from %d (every event is checked daily)", o.plan.from)
	}
	if !o.anchors {
		scope += "; anchors aren't configured (AUDIT_ANCHOR_BUCKET), so only the hash chain is checked"
	}
	return scope
}

// recordedStatus is the status the Audit Logs page shows after a run, from the state the run left. Times a
// restarted gateway hasn't seen yet come from the previous status: they're only shown.
func recordedStatus(
	previous *db.AuditVerificationStatus,
	o *outcome,
	state *runState,
	now time.Time,
) *db.AuditVerificationStatus {
	status := &db.AuditVerificationStatus{
		CheckedAt:    now,
		Result:       o.result(),
		FailureCount: o.failureCount(),
		WarningCount: o.warningCount(),
		Summary:      o.summary(),
	}
	if previous != nil {
		status.FullCheckedAt = previous.FullCheckedAt
		status.LastPassedAt = previous.LastPassedAt
	}
	if !state.fullCheckedAt.IsZero() {
		fullCheckedAt := state.fullCheckedAt
		status.FullCheckedAt = &fullCheckedAt
	}
	if o.passed() {
		status.LastPassedAt = &now
	} else {
		failingSince := state.failingSince
		status.FailingSince = &failingSince
	}
	return status
}

// logOutcome writes a line for every run, so the log shows verification running and what it found.
func logOutcome(o *outcome, status *db.AuditVerificationStatus) {
	if o.passed() {
		gtwlog.Infof("%s. %s", o.headline(), o.scope())
		return
	}
	gtwlog.Errorf("%s (failing since %s). %s",
		o.headline(), status.FailingSince.UTC().Format(time.RFC3339), o.scope())
}
