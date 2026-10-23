package verifyaudit

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

// A failure the last alert didn't report is alerted, even when the count stays the same: one tampered event put
// back, another tampered.
func TestRunAlertsAFailureThatReplacesAnother(t *testing.T) {
	h := newHarness(t, nil)
	h.writeEvents(1)
	h.runAt(firstRun)

	// Events written after the last passing run, which the next hourly run checks
	events := h.writeEvents(2)
	h.setAction(events[0], "app.delete")
	h.runAt(firstRun.Add(time.Hour))
	require.Len(t, h.slackAlerts(), 1)

	h.setAction(events[0], "app.list")
	h.setAction(events[1], "app.delete")
	status := h.runAt(firstRun.Add(2 * time.Hour))
	assert.Equal(t, 1, status.FailureCount)
	alerts := h.slackAlerts()
	require.Len(t, alerts, 2, "the new failure is alerted")
	assert.Contains(t, alerts[1].Details, fmt.Sprintf("event %d (id", events[1]))

	h.runAt(firstRun.Add(3 * time.Hour))
	assert.Len(t, h.slackAlerts(), 2, "the same failure isn't alerted again within the day")
}

// Past the failure keys a report keeps, a failure that replaces another is still alerted.
func TestRunAlertsAFailureThatReplacesAnotherPastTheKeyCap(t *testing.T) {
	h := newHarness(t, nil)
	events := h.writeEvents(1003)
	first, last := events[1], events[len(events)-1]
	h.tamper(fmt.Sprintf(`UPDATE audit.audit_event SET action = 'app.delete' WHERE chain_index BETWEEN %d AND %d`,
		first, last))
	status := h.runAt(firstRun)
	require.Equal(t, 1002, status.FailureCount)
	require.Len(t, h.slackAlerts(), 1)

	h.setAction(last, "app.list")
	h.setAction(events[0], "app.delete")
	status = h.runAt(firstRun.Add(time.Hour))
	assert.Equal(t, 1002, status.FailureCount)
	require.Len(t, h.slackAlerts(), 2, "the new failure is alerted")

	h.runAt(firstRun.Add(2 * time.Hour))
	assert.Len(t, h.slackAlerts(), 2, "the same failures aren't alerted again within the day")
}

// An alert whose Slack notification can't be queued isn't counted as sent: the next run sends it again. So is a
// recovery alert.
func TestRunSendsAlertsSlackDidntGetAgain(t *testing.T) {
	h := newHarness(t, nil)
	h.writeEvents(1)
	h.runAt(firstRun)

	events := h.writeEvents(2)
	h.slack.err = errors.New("queue unavailable")
	h.setAction(events[0], "app.delete")
	h.runAt(firstRun.Add(time.Hour))
	assert.Empty(t, h.slack.ids)
	h.slack.err = nil
	h.runAt(firstRun.Add(2 * time.Hour))
	alerts := h.slackAlerts()
	require.Len(t, alerts, 1, "the failure alert is sent by the next run")
	assert.Equal(t, FailureAction, alerts[0].Action)

	h.setAction(events[0], "app.list")
	h.slack.err = errors.New("queue unavailable")
	status := h.runAt(firstRun.Add(3 * time.Hour))
	assert.Equal(t, db.AuditVerificationPassed, status.Result)
	h.slack.err = nil
	h.runAt(firstRun.Add(4 * time.Hour))
	alerts = h.slackAlerts()
	require.Len(t, alerts, 2, "the recovery alert is sent by the next run")
	assert.Equal(t, RecoveryAction, alerts[1].Action)

	h.runAt(firstRun.Add(5 * time.Hour))
	assert.Len(t, h.slackAlerts(), 2, "and only once")
}

// A run that finds failures and then can't finish (here, the anchors can't be listed) keeps the failures in its
// status and its alert, with the error that stopped it.
func TestRunThatStopsKeepsTheFailuresItFound(t *testing.T) {
	h := newHarness(t, unreachableStore{fakes3.New(firstRun)})
	events := h.writeEvents(2)
	h.setAction(events[1], "app.delete")

	status := h.runAt(firstRun)
	assert.Equal(t, db.AuditVerificationError, status.Result)
	assert.Equal(t, 1, status.FailureCount)
	failure := fmt.Sprintf("event %d (id", events[1])
	assert.Contains(t, status.Summary, failure)
	assert.Contains(t, status.Summary, "not allowed to list object versions")
	assert.Contains(t, status.Summary, "Result: FAILED (1 integrity failures, and stopped before it finished")
	alerts := h.slackAlerts()
	require.Len(t, alerts, 1)
	assert.Contains(t, alerts[0].Details, failure)
	assert.Contains(t, alerts[0].Details, "not allowed to list object versions")
}
