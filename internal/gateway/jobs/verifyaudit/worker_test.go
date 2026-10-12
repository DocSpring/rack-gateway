package verifyaudit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

func TestRunPassesAnIntactChainWithoutAlerting(t *testing.T) {
	h := newAnchoredHarness(t)
	h.writeEvents(3)
	h.now = firstRun.Add(-30 * time.Minute)
	h.writeAnchor()

	status := h.runAt(firstRun)
	assert.Equal(t, db.AuditVerificationPassed, status.Result)
	assert.Zero(t, status.FailureCount)
	assert.Contains(t, status.Summary, "Checking every event")
	assert.Contains(t, status.Summary, "Anchors: 1 checked")
	assert.Contains(t, status.Summary, "Result: OK")
	require.NotNil(t, status.FullCheckedAt)
	assert.WithinDuration(t, firstRun, *status.FullCheckedAt, 0)
	assert.Nil(t, status.FailingSince)
	require.NotNil(t, h.worker.state.verified)
	assert.Equal(t, int64(2), h.worker.state.verified.Index)

	assert.Empty(t, h.sentryEvents)
	assert.Empty(t, h.slack.ids)
}

// A failure is alerted once, then only when there are more failures or a day later; Sentry gets every failing run
// as one issue; verification passing again is alerted too.
func TestRunAlertsOnFailureAndRecovery(t *testing.T) {
	h := newAnchoredHarness(t)
	h.writeEvents(3)
	h.now = firstRun.Add(-30 * time.Minute)
	h.writeAnchor()
	h.runAt(firstRun)

	// An event written after the last passing run is changed: the next hourly run checks it.
	tampered := h.writeEvents(2)[1]
	h.setAction(tampered, "app.delete")
	status := h.runAt(firstRun.Add(time.Hour))
	assert.Equal(t, db.AuditVerificationFailed, status.Result)
	assert.Equal(t, 1, status.FailureCount)
	assert.Contains(t, status.Summary, fmt.Sprintf("Checking the events from %d", tampered-1))
	assert.Contains(t, status.Summary, fmt.Sprintf("event %d (id", tampered))
	require.NotNil(t, status.FailingSince)
	assert.WithinDuration(t, firstRun.Add(time.Hour), *status.FailingSince, 0)
	assert.True(t, h.worker.state.plan(h.now).full, "the next run checks every event")

	require.Len(t, h.sentryEvents, 1)
	assert.Equal(t, []string{"audit-log-verification", db.AuditVerificationFailed}, h.sentryEvents[0].Fingerprint)
	assert.Contains(t, h.sentryEvents[0].Message, "1 integrity failures")
	alerts := h.slackAlerts()
	require.Len(t, alerts, 1)
	assert.Equal(t, "audit_log.verify", alerts[0].Action)
	assert.Equal(t, audit.StatusFailed, alerts[0].Status)
	assert.Equal(t, audit.SystemActor, alerts[0].UserEmail)
	assert.Contains(t, alerts[0].Details, fmt.Sprintf("event %d (id", tampered))

	// The same failure an hour later: Sentry only.
	status = h.runAt(firstRun.Add(2 * time.Hour))
	assert.Equal(t, 1, status.FailureCount)
	assert.Contains(t, status.Summary, "Checking every event")
	require.Len(t, h.sentryEvents, 2)
	assert.Equal(t, h.sentryEvents[0].Fingerprint, h.sentryEvents[1].Fingerprint, "one Sentry issue")
	assert.Len(t, h.slack.ids, 1, "the same failure isn't alerted again within the hour")
	assert.WithinDuration(t, firstRun.Add(time.Hour), *status.FailingSince, 0)

	// Another changed event is a new failure, alerted straight away.
	h.setAction(tampered-1, "app.delete")
	h.now = firstRun.Add(150 * time.Minute)
	h.writeAnchor()
	status = h.runAt(firstRun.Add(3 * time.Hour))
	assert.Equal(t, 2, status.FailureCount)
	assert.Len(t, h.slack.ids, 2, "more failures than the last alert reported")

	// Still failing a day after the last alert: a reminder.
	h.now = firstRun.Add(27*time.Hour - 30*time.Minute)
	h.writeAnchor()
	status = h.runAt(firstRun.Add(27 * time.Hour))
	assert.Equal(t, 2, status.FailureCount)
	assert.Len(t, h.slack.ids, 3, "reminded a day after the last alert")

	// Fixed: one recovery alert.
	h.setAction(tampered-1, "app.list")
	h.setAction(tampered, "app.list")
	status = h.runAt(firstRun.Add(28 * time.Hour))
	assert.Equal(t, db.AuditVerificationPassed, status.Result)
	assert.Nil(t, status.FailingSince)
	alerts = h.slackAlerts()
	require.Len(t, alerts, 4)
	assert.Equal(t, audit.StatusSuccess, alerts[3].Status)
	assert.Contains(t, alerts[3].Details, `"result":"passed"`)
	assert.Len(t, h.sentryEvents, 4, "a passing run isn't sent to Sentry")

	h.runAt(firstRun.Add(29 * time.Hour))
	assert.Len(t, h.slack.ids, 4, "only the first passing run is alerted")
}

func TestRunWithoutAnchorsChecksTheChainOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.writeEvents(3)

	status := h.runAt(firstRun)
	assert.Equal(t, db.AuditVerificationPassed, status.Result)
	assert.Contains(t, status.Summary,
		"anchors aren't configured (AUDIT_ANCHOR_BUCKET), so only the hash chain is checked")
	assert.NotContains(t, status.Summary, "Anchors:")
	assert.Empty(t, h.sentryEvents)
	assert.Empty(t, h.slack.ids, "missing anchors aren't alerted when anchors aren't configured")
}

// The hourly runs check the events since the last passing run; a change to an older event is found by the daily
// run that checks every event.
func TestRunChecksEveryEventDaily(t *testing.T) {
	h := newHarness(t, nil)
	old := h.writeEvents(3)[0]
	h.runAt(firstRun)

	h.setAction(old, "app.delete")
	h.writeEvents(1)
	status := h.runAt(firstRun.Add(time.Hour))
	assert.Equal(t, db.AuditVerificationPassed, status.Result)
	assert.Contains(t, status.Summary, "Checking the events from 3")
	require.NotNil(t, h.worker.state.verified)
	assert.Equal(t, int64(3), h.worker.state.verified.Index)
	assert.WithinDuration(t, firstRun, *status.FullCheckedAt, 0, "only a run that checks every event counts")

	status = h.runAt(firstRun.Add(FullCheckInterval))
	assert.Equal(t, db.AuditVerificationFailed, status.Result)
	assert.Contains(t, status.Summary, "Checking every event")
	assert.Contains(t, status.Summary, fmt.Sprintf("event %d (id", old))
}

// What the worker has verified and alerted is kept in memory, where someone who can write to the database can't
// change it. A restarted gateway checks every event, whatever the recorded status says, and alerts again; passing
// after a restart is still alerted.
func TestRunAfterARestart(t *testing.T) {
	h := newHarness(t, nil)
	old := h.writeEvents(3)[0]
	h.runAt(firstRun)

	h.setAction(old, "app.delete")
	h.now = firstRun.Add(time.Hour)
	require.NoError(t, h.database.SaveAuditVerificationStatus(context.Background(), &db.AuditVerificationStatus{
		CheckedAt:     h.now,
		Result:        db.AuditVerificationPassed,
		Summary:       "Result: OK\n",
		FullCheckedAt: &h.now,
		LastPassedAt:  &h.now,
	}))
	h.restart()
	status := h.runAt(firstRun.Add(90 * time.Minute))
	assert.Equal(t, db.AuditVerificationFailed, status.Result)
	assert.Contains(t, status.Summary, "Checking every event")
	assert.Len(t, h.slack.ids, 1)

	h.restart()
	h.runAt(firstRun.Add(150 * time.Minute))
	assert.Len(t, h.slack.ids, 2, "still failing after a restart: alerted again")

	h.setAction(old, "app.list")
	h.restart()
	status = h.runAt(firstRun.Add(210 * time.Minute))
	assert.Equal(t, db.AuditVerificationPassed, status.Result)
	alerts := h.slackAlerts()
	require.Len(t, alerts, 3)
	assert.Equal(t, audit.StatusSuccess, alerts[2].Status, "passing after a restart is alerted")
}

// Removing events from the end leaves a valid chain, but not one that holds the last event a run checked, even
// once new events take their places.
func TestRunFailsWhenEventsAreRemovedFromTheEnd(t *testing.T) {
	h := newHarness(t, nil)
	h.writeEvents(4)
	h.runAt(firstRun)

	h.tamper(`DELETE FROM audit.audit_event WHERE chain_index >= 2`)
	status := h.runAt(firstRun.Add(time.Hour))
	assert.Equal(t, db.AuditVerificationFailed, status.Result)
	assert.Contains(t, status.Summary,
		"the chain ends at event 1, but an earlier verification checked it through event 3")
	assert.Len(t, h.slack.ids, 1)

	h.writeEvents(2)
	status = h.runAt(firstRun.Add(2 * time.Hour))
	assert.Equal(t, db.AuditVerificationFailed, status.Result)
	assert.Contains(t, status.Summary, "event 3 isn't the event an earlier verification checked")
}

func TestRunRecordsWhyItCouldNotVerify(t *testing.T) {
	h := newHarness(t, unreachableStore{fakes3.New(firstRun)})
	h.writeEvents(1)

	status := h.runAt(firstRun)
	assert.Equal(t, db.AuditVerificationError, status.Result)
	assert.Contains(t, status.Summary, "Audit log verification couldn't run")
	assert.Contains(t, status.Summary, "AccessDenied")
	require.Len(t, h.sentryEvents, 1)
	assert.Equal(t, []string{"audit-log-verification", db.AuditVerificationError}, h.sentryEvents[0].Fingerprint)
	alerts := h.slackAlerts()
	require.Len(t, alerts, 1)
	assert.Equal(t, audit.StatusError, alerts[0].Status)
	assert.Contains(t, alerts[0].Details, "AccessDenied")
}

// Anchor versions never change, so each is fetched once; a new version of an anchor is still fetched and reported.
func TestRunFetchesEachAnchorVersionOnce(t *testing.T) {
	store := &countingStore{Bucket: fakes3.New(firstRun.Add(-3 * time.Hour))}
	h := newHarness(t, store)
	h.bucket = store.Bucket
	h.writeEvents(1)
	for _, before := range []time.Duration{150 * time.Minute, 90 * time.Minute, 30 * time.Minute} {
		h.now = firstRun.Add(-before)
		h.writeAnchor()
	}

	status := h.runAt(firstRun)
	assert.Equal(t, db.AuditVerificationPassed, status.Result, status.Summary)
	assert.Equal(t, 3, store.gets)

	status = h.runAt(firstRun.Add(time.Hour))
	assert.Equal(t, db.AuditVerificationPassed, status.Result, status.Summary)
	assert.Equal(t, 3, store.gets, "anchors read before aren't fetched again")

	h.bucket.PutAt(jobaudit.AnchorKey(testChainID, firstRun), []byte(`{"chain_id":"production"}`),
		firstRun.Add(61*time.Minute))
	status = h.runAt(firstRun.Add(2 * time.Hour))
	assert.Equal(t, 4, store.gets)
	assert.Contains(t, status.Summary, "has 2 versions")
}

func TestClockScheduleRunsAtTheSameMinuteEveryHour(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 5, 1, hour, minute, 0, 0, time.UTC) }
	schedule := clockSchedule{interval: Interval, offset: scheduleOffset}
	assert.Equal(t, at(10, 30), schedule.Next(at(10, 0)))
	assert.Equal(t, at(10, 30), schedule.Next(at(10, 29)))
	assert.Equal(t, at(11, 30), schedule.Next(at(10, 30)))
	assert.Equal(t, at(11, 30), schedule.Next(at(10, 45)))
}

func TestRunStatePlan(t *testing.T) {
	now := firstRun
	verified := &auditverify.VerifiedEvent{Index: 41, Hash: []byte("hash of event 41")}
	recently := now.Add(-time.Hour)

	assert.Equal(t, runPlan{full: true}, (&runState{}).plan(now), "the first run")
	assert.Equal(t, runPlan{from: 42, verified: verified},
		(&runState{verified: verified, fullCheckedAt: recently}).plan(now))
	assert.Equal(t, runPlan{full: true, verified: verified},
		(&runState{verified: verified, fullCheckedAt: now.Add(-FullCheckInterval)}).plan(now),
		"a day after the last run that checked every event")
	assert.Equal(t, runPlan{full: true, verified: verified},
		(&runState{verified: verified, fullCheckedAt: recently, failingSince: recently}).plan(now),
		"while failing")
}
