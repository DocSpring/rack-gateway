package verifyaudit

import (
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// FullCheckInterval is how often a run checks every event. The hourly runs in between check the events written
// since the last passing run, linked to the last event it checked, that this event is still there unchanged, and
// every anchor against the database. A change to an earlier event is then found by the next full check, within a
// day, unless an anchor records that event; everything else is found within the hour. Checking every event every
// hour would read the whole audit table, which only grows, every hour.
const FullCheckInterval = 24 * time.Hour

// ReminderInterval is how often a failure is alerted again while verification keeps failing without new
// failures. Some failures can't be fixed: a locked anchor that an earlier version wrote twice is reported until
// its retention ends.
const ReminderInterval = 24 * time.Hour

// runState is what the worker remembers between runs: how far it has verified the chain, and what it has alerted
// about. It's kept in memory, not in the database: verification is there to catch someone who can write to the
// database, who could otherwise use a stored state to keep runs from checking every event, or failures from being
// alerted. So the first run after the gateway starts checks every event, and alerts again about a failure that
// was alerted before the restart.
type runState struct {
	// verified is the latest event a passing run checked (nil before one has, or when the chain was empty).
	verified *auditverify.VerifiedEvent
	// fullCheckedAt is when the last passing run that checked every event started (zero: none yet).
	fullCheckedAt time.Time
	// failingSince is when the current failing streak started (zero: not failing).
	failingSince time.Time
	// alertedAt, alertedFailures and alertedKeys describe the streak's latest failure alert.
	alertedAt       time.Time
	alertedFailures int
	alertedKeys     map[string]bool
	// recoveryPendingSince is the start of a failing streak whose recovery alert couldn't be recorded: the next
	// passing run sends it again (zero: none pending).
	recoveryPendingSince time.Time
}

// plan checks every event on the first run, while verification is failing and once a day, and otherwise the
// events since the last passing run.
func (s *runState) plan(now time.Time) runPlan {
	plan := runPlan{verified: s.verified}
	if s.verified == nil || !s.failingSince.IsZero() || now.Sub(s.fullCheckedAt) >= FullCheckInterval {
		plan.full = true
		return plan
	}
	plan.from = s.verified.Index + 1
	return plan
}

// streakStart is when the current failing streak started: as this gateway saw it, or as the previous status
// recorded it when the gateway restarted during the streak (only shown), or now.
func (s *runState) streakStart(previous *db.AuditVerificationStatus, now time.Time) time.Time {
	switch {
	case !s.failingSince.IsZero():
		return s.failingSince
	case !s.recoveryPendingSince.IsZero():
		return s.recoveryPendingSince
	case previous != nil && previous.FailingSince != nil:
		return *previous.FailingSince
	default:
		return now
	}
}

// record updates the state after a run. since is when the failing streak started; kind is the alert the run called
// for, and alerted whether it was recorded.
func (s *runState) record(o *outcome, now, since time.Time, kind alertKind, alerted bool) {
	if o.passed() {
		*s = runState{fullCheckedAt: s.fullCheckedAt}
		if kind == recoveryAlert && !alerted {
			s.recoveryPendingSince = since
		}
		if head := o.report.Head; !head.Empty {
			s.verified = &auditverify.VerifiedEvent{Index: head.LastIndex, Hash: head.LastHash}
		}
		if o.plan.full {
			s.fullCheckedAt = now
		}
		return
	}
	// The last verified event stays: a failing run doesn't move it, and it keeps the chain to it checked.
	s.failingSince = since
	s.recoveryPendingSince = time.Time{}
	if kind == failureAlert && alerted {
		s.alertedAt = now
		s.alertedFailures = o.failureCount()
		s.alertedKeys = map[string]bool{}
		if o.report != nil {
			for _, key := range o.report.FailureKeys() {
				s.alertedKeys[key] = true
			}
		}
	}
}
