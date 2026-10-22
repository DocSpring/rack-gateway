// Package verifyaudit runs the audit log verification (the checks `rack-gateway-api verify-audit` runs) every hour
// inside the gateway, records each run's outcome and alerts when verification fails.
package verifyaudit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Interval is how often the gateway verifies its audit log.
const Interval = time.Hour

// scheduleOffset is how far into each hour (UTC) a run starts. Runs follow the clock rather than an interval from
// startup: the gateway restarts on every deploy, which would restart an interval timer, and a run as the gateway
// starts would race the anchor writer, which also runs then (after hours of downtime the latest anchor is too old
// until it writes the next one).
const scheduleOffset = 30 * time.Minute

// runTimeout bounds a run. River's default, a minute, is too short for the first run after a restart, which reads
// every anchor in the bucket.
const runTimeout = 30 * time.Minute

// recordTimeout bounds recording a run's outcome, which happens even when the run itself timed out.
const recordTimeout = 30 * time.Second

// Args schedules a verification run.
type Args struct{}

// Kind returns the unique identifier for this job type.
func (Args) Kind() string { return "audit:verify" }

// Database is what a run reads and records. *db.Database implements it.
type Database interface {
	auditverify.ChainSource
	AuditVerificationStatus(ctx context.Context) (*db.AuditVerificationStatus, error)
	SaveAuditVerificationStatus(ctx context.Context, status *db.AuditVerificationStatus) error
}

// AuditFunc records an alert as an audit event. The gateway passes its audit logger's LogAlert, which also sends the
// event to the Slack channels whose action patterns match it, and returns an error when that can't be queued.
type AuditFunc func(*db.AuditLog) error

// HeartbeatEnv is the URL of the verification's heartbeat monitor, pinged after every run (unset: no heartbeat).
const HeartbeatEnv = "AUDIT_VERIFY_HEARTBEAT_URL"

// HeartbeatFunc pings the verification's heartbeat monitor (HeartbeatEnv).
type HeartbeatFunc func(context.Context) error

// heartbeatTimeout bounds the ping after a run, which happens even when the run itself timed out.
const heartbeatTimeout = 15 * time.Second

// Worker runs the scheduled verification.
type Worker struct {
	river.WorkerDefaults[Args]
	database Database
	opts     auditverify.Options
	audit    AuditFunc
	// heartbeat is pinged after every run (nil: no heartbeat).
	heartbeat HeartbeatFunc
	// capture sends an event to Sentry.
	capture func(*sentry.Event)
	now     func() time.Time

	// mu serializes runs, which share state.
	mu    sync.Mutex
	state runState
}

// NewWorker returns the worker. opts is verify-audit's configuration (see jobs.AuditVerifyOptions); auditFn
// records alerts as audit events, which reach Slack the way security alerts do; heartbeat, unless nil, is pinged
// after every run, so a monitor can alert when runs stop.
func NewWorker(database Database, opts auditverify.Options, auditFn AuditFunc, heartbeat HeartbeatFunc) *Worker {
	if opts.Anchors != nil {
		anchors := *opts.Anchors
		anchors.Store = newAnchorCache(anchors.Store)
		opts.Anchors = &anchors
	}
	return &Worker{
		database:  database,
		opts:      opts,
		audit:     auditFn,
		heartbeat: heartbeat,
		capture:   func(event *sentry.Event) { sentry.CaptureEvent(event) },
		now:       time.Now,
	}
}

// PeriodicJob schedules a run every hour, scheduleOffset past the hour. A run that fails to finish isn't retried:
// the next one is due within the hour.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		clockSchedule{interval: Interval, offset: scheduleOffset},
		func() (river.JobArgs, *river.InsertOpts) { return Args{}, &river.InsertOpts{MaxAttempts: 1} },
		nil,
	)
}

// clockSchedule runs offset into every interval of the clock (UTC).
type clockSchedule struct {
	interval time.Duration
	offset   time.Duration
}

// Next returns the first run time after current.
func (s clockSchedule) Next(current time.Time) time.Time {
	next := current.Truncate(s.interval).Add(s.offset)
	if !next.After(current) {
		next = next.Add(s.interval)
	}
	return next
}

// Timeout bounds a run (see runTimeout).
func (*Worker) Timeout(*river.Job[Args]) time.Duration { return runTimeout }

// Work runs one verification. It only fails when the status can't be read or recorded, or the gateway is shutting
// down: a failing verification is recorded and alerted, not retried.
func (w *Worker) Work(ctx context.Context, _ *river.Job[Args]) error {
	return w.Run(ctx)
}

// Run verifies the audit log, sends a failure to Sentry, sends the alert the outcome calls for and records the
// outcome for the Audit Logs page.
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	previous, err := w.database.AuditVerificationStatus(ctx)
	if err != nil {
		return err
	}
	now := w.now()
	result, err := w.verify(ctx, w.state.plan(now), now)
	if err != nil {
		return err
	}

	alert := w.state.decideAlert(previous, result, now)
	since := w.state.streakStart(previous, now)
	if !result.passed() {
		w.capture(sentryEvent(result, since))
	}
	alerted := w.sendAlert(alert, result, since)
	w.state.record(result, now, since, alert, alerted)
	status := recordedStatus(previous, result, &w.state, now)

	// A run that timed out is still recorded, with a context of its own.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if err := w.database.SaveAuditVerificationStatus(recordCtx, status); err != nil {
		return err
	}
	logOutcome(result, status)
	w.pingHeartbeat(ctx)
	return nil
}

// pingHeartbeat tells the heartbeat monitor a run finished, whatever it found: failures alert through Sentry and
// Slack, and the monitor alerts when runs stop.
func (w *Worker) pingHeartbeat(ctx context.Context) {
	if w.heartbeat == nil {
		return
	}
	pingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), heartbeatTimeout)
	defer cancel()
	if err := w.heartbeat(pingCtx); err != nil {
		gtwlog.Errorf("Audit verification: %v", err)
	}
}

// verify runs the checks the plan calls for. It returns an error only when the gateway is shutting down, which
// says nothing about the audit log; anything else that stops the run is its outcome.
func (w *Worker) verify(ctx context.Context, plan runPlan, now time.Time) (*outcome, error) {
	opts := w.opts
	opts.From = plan.from
	opts.VerifiedThrough = plan.verified
	opts.Now = now
	report, err := auditverify.Run(ctx, w.database, opts)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return nil, fmt.Errorf("audit verification canceled: %w", err)
	}
	return &outcome{plan: plan, anchors: opts.Anchors != nil, report: report, err: err}, nil
}
