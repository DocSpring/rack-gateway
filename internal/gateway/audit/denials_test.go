package audit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

// throttleHarness drives a denialThrottle with a manual clock and timers, and keeps what it writes.
type throttleHarness struct {
	throttle *denialThrottle
	written  []*db.AuditLog
	timers   []func()
	now      time.Time
}

func newThrottleHarness() *throttleHarness {
	h := &throttleHarness{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	h.throttle = newDenialThrottle(func(entry *db.AuditLog) { h.written = append(h.written, entry) })
	h.throttle.now = func() time.Time { return h.now }
	h.throttle.schedule = func(_ time.Duration, f func()) { h.timers = append(h.timers, f) }
	return h
}

func (h *throttleHarness) deny(email, action string) {
	h.throttle.record(&db.AuditLog{UserEmail: email, Action: action, Details: `{"reason":"no"}`})
}

func (h *throttleHarness) fireTimers() {
	timers := h.timers
	h.timers = nil
	for _, fire := range timers {
		fire()
	}
}

func (h *throttleHarness) counts() []int {
	counts := make([]int, 0, len(h.written))
	for _, entry := range h.written {
		counts = append(counts, entry.EventCount)
	}
	return counts
}

// TestRepeatedDenialsCollapseIntoOneCount: N denials of one action by one user within a minute are the first
// denial (written at once) and one entry counting the other N-1, written when the minute is up.
func TestRepeatedDenialsCollapseIntoOneCount(t *testing.T) {
	h := newThrottleHarness()
	for i := 0; i < 5; i++ {
		h.deny("viewer@example.com", "user.create")
		h.now = h.now.Add(10 * time.Second)
	}
	require.Equal(t, []int{1}, h.counts(), "only the first denial is written at once")

	h.fireTimers()
	require.Equal(t, []int{1, 4}, h.counts())
	var details map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(h.written[1].Details), &details))
	require.Equal(t, "no", details["reason"])
	require.Equal(t, "2026-01-02T03:04:15Z", details["repeats_from"])
	require.Equal(t, "2026-01-02T03:04:45Z", details["repeats_until"])

	h.fireTimers()
	require.Len(t, h.written, 2, "a window is written once")
}

// TestADenialAfterTheWindowIsWrittenAgain: once a minute has passed, the next denial is a new first denial,
// even before the old window's timer fires (which then has nothing left to write).
func TestADenialAfterTheWindowIsWrittenAgain(t *testing.T) {
	h := newThrottleHarness()
	h.deny("viewer@example.com", "user.create")
	h.now = h.now.Add(30 * time.Second)
	h.deny("viewer@example.com", "user.create")
	h.now = h.now.Add(31 * time.Second)
	h.deny("viewer@example.com", "user.create")

	require.Equal(t, []int{1, 1, 1}, h.counts(), "first denial, the old window's one repeat, new first denial")
	require.Contains(t, h.written[1].Details, "repeats_from")
	require.NotContains(t, h.written[2].Details, "repeats_from")

	h.fireTimers()
	require.Len(t, h.written, 3)
}

// TestDenialsAreThrottledPerPrincipalAndAction: another action, or another API token of the same owner, isn't a
// repeat.
func TestDenialsAreThrottledPerPrincipalAndAction(t *testing.T) {
	h := newThrottleHarness()
	h.deny("viewer@example.com", "user.create")
	h.deny("viewer@example.com", "user.delete")
	h.deny("other@example.com", "user.create")

	tokenDenial := func(id int64) *db.AuditLog {
		return &db.AuditLog{UserEmail: "admin@example.com", APITokenID: &id, Action: "app.delete"}
	}
	h.throttle.record(tokenDenial(7))
	h.throttle.record(tokenDenial(8))
	h.throttle.record(tokenDenial(7))

	require.Equal(t, []int{1, 1, 1, 1, 1}, h.counts())
	h.fireTimers()
	require.Equal(t, []int{1, 1, 1, 1, 1, 1}, h.counts(), "token 7's repeat")
}

// TestDenialsAgainstDifferentTargetsAreNotRepeats: a denial against another resource or rack path is written at
// once, so denials against one target can't hide a denial against another.
func TestDenialsAgainstDifferentTargetsAreNotRepeats(t *testing.T) {
	h := newThrottleHarness()
	proxied := func(resource, path string) *db.AuditLog {
		return &db.AuditLog{
			UserEmail: "viewer@example.com", Action: "env.read", Resource: resource,
			Details: `{"method":"GET","path":"` + path + `"}`,
		}
	}
	h.throttle.record(proxied("web", "/apps/web/environment"))
	h.throttle.record(proxied("billing", "/apps/billing/environment"))
	h.throttle.record(proxied("web", "/apps/web/environment"))
	h.throttle.record(proxied("web", "/apps/web/releases"))

	require.Equal(t, []int{1, 1, 1}, h.counts(), "each new target's first denial is written at once")
	require.Contains(t, h.written[1].Details, "/apps/billing/environment")
	h.fireTimers()
	require.Equal(t, []int{1, 1, 1, 1}, h.counts(), "the repeat against web's environment")
}

func TestFlushWritesTheRepeatsBeingCounted(t *testing.T) {
	h := newThrottleHarness()
	h.deny("viewer@example.com", "user.create")
	h.deny("viewer@example.com", "user.create")
	h.deny("viewer@example.com", "user.create")

	h.throttle.flush()
	require.Equal(t, []int{1, 2}, h.counts())
	h.fireTimers()
	require.Len(t, h.written, 2, "a flushed window isn't written again")
}

// TestRecordDenialFailsOpen: a denial the audit log can't take is reported, and the caller carries on.
func TestRecordDenialFailsOpen(t *testing.T) {
	logger := NewLogger(nil)
	logger.SetChainWriter(audittest.FailingChainWriter{})
	entry := &db.AuditLog{UserEmail: "viewer@example.com", Action: "user.create"}

	logger.RecordDenial(entry)
	require.Equal(t, StatusDenied, entry.Status)
	require.Equal(t, "deny", entry.RBACDecision)
	logger.FlushDenials()
}

func TestWithDetails(t *testing.T) {
	require.JSONEq(t, `{"a":1,"reason":"x"}`, WithDetails(`{"a":1}`, map[string]interface{}{"reason": "x"}))
	require.JSONEq(t, `{"reason":"x"}`, WithDetails("", map[string]interface{}{"reason": "x"}))
	require.Equal(t, "not json", WithDetails("not json", map[string]interface{}{"reason": "x"}))
}
