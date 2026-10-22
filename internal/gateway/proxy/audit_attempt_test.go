package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

const attemptUserEmail = "deployer@example.com"

// countingRack answers every request with a build and counts the requests that reached it.
type countingRack struct {
	calls atomic.Int32
}

func (rk *countingRack) handler(w http.ResponseWriter, _ *http.Request) {
	rk.calls.Add(1)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"B1","status":"created"}`)
}

func newAttemptProxy(t *testing.T) (*Handler, *db.Database, *countingRack, func()) {
	t.Helper()
	rack := &countingRack{}
	h, database, mgr, cleanup := newProxyWithRackServer(t, rack.handler)
	deployer := &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}
	require.NoError(t, mgr.CreateUser(attemptUserEmail, deployer))
	return h, database, rack, cleanup
}

// attemptPromotePath is a state-changing rack call that takes no body. Without image patterns or a deploy approval,
// the gateway forwards it without reading the release first.
const attemptPromotePath = "/apps/my-app/releases/R1/promote"

func proxyAs(t *testing.T, h *Handler, database *db.Database, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(requestAs(t, database, attemptUserEmail).Context())
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	return rr
}

func attemptEntries(t *testing.T, database *db.Database) []*db.AuditLog {
	t.Helper()
	logs, err := database.GetAuditLogs(attemptUserEmail, time.Time{}, 20)
	require.NoError(t, err)
	sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })
	return logs
}

// TestProxiedRequestsAreRefusedWhenTheAuditLogIsDown: a rack call whose attempt can't be recorded gets a 503 and
// never reaches the rack, reads included.
func TestProxiedRequestsAreRefusedWhenTheAuditLogIsDown(t *testing.T) {
	cases := map[string]struct{ method, path, action string }{
		"promote":    {http.MethodPost, attemptPromotePath, "release.promote"},
		"build list": {http.MethodGet, "/apps/my-app/builds", "build.list"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, database, rack, cleanup := newAttemptProxy(t)
			defer cleanup()
			h.auditLogger.SetChainWriter(audittest.FailingChainWriter{})

			rr := proxyAs(t, h, database, tc.method, tc.path)
			require.Equal(t, http.StatusServiceUnavailable, rr.Code, rr.Body.String())
			require.Contains(t, rr.Body.String(), "audit log unavailable; refusing to perform "+tc.action)
			require.Zero(t, rack.calls.Load(), "the request must not be forwarded")
		})
	}
}

// TestProxiedMutationRecordsAttemptAndResult: the attempt is written before the rack call and the result after,
// and they share the request ID.
func TestProxiedMutationRecordsAttemptAndResult(t *testing.T) {
	h, database, _, cleanup := newAttemptProxy(t)
	defer cleanup()

	rr := proxyAs(t, h, database, http.MethodPost, attemptPromotePath)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	entries := attemptEntries(t, database)
	require.GreaterOrEqual(t, len(entries), 2)
	attempt, result := entries[0], entries[1]
	require.Equal(t, audit.StatusAttempted, attempt.Status)
	require.Equal(t, "release.promote", attempt.Action)
	require.Equal(t, audit.StatusSuccess, result.Status)
	require.Equal(t, "release.promote", result.Action)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.NotEmpty(t, attempt.RequestID)
	require.Equal(t, attempt.RequestID, result.RequestID)
	require.NotContains(t, attempt.Details, "request_id", "the request ID is in its own column")
}

// TestProxiedFailureAfterTheAttemptIsRecorded: when the rack can't be reached after the attempt was written, the
// failure is recorded as the attempt's result.
func TestProxiedFailureAfterTheAttemptIsRecorded(t *testing.T) {
	h, database, _, cleanup := newAttemptProxy(t)
	cleanup() // the rack is gone

	rr := proxyAs(t, h, database, http.MethodPost, attemptPromotePath)
	require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())

	entries := attemptEntries(t, database)
	require.Len(t, entries, 2)
	require.Equal(t, audit.StatusAttempted, entries[0].Status)
	require.Equal(t, audit.StatusError, entries[1].Status)
	require.Equal(t, http.StatusInternalServerError, entries[1].HTTPStatus)
	require.Equal(t, entries[0].RequestID, entries[1].RequestID)
}

// TestProxiedDenialsAreRecordedAndThrottled: RBAC denials of rack calls are in the audit log (they used to go to
// the request log only), with repeats counted like the gateway API's.
func TestProxiedDenialsAreRecordedAndThrottled(t *testing.T) {
	h, database, rack, cleanup := newAttemptProxy(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		rr := proxyAs(t, h, database, http.MethodDelete, "/apps/my-app")
		require.Equal(t, http.StatusForbidden, rr.Code)
	}
	require.Zero(t, rack.calls.Load())

	entries := attemptEntries(t, database)
	require.Len(t, entries, 1)
	require.Equal(t, "app.delete", entries[0].Action)
	require.Equal(t, audit.StatusDenied, entries[0].Status)
	require.Equal(t, "deny", entries[0].RBACDecision)
	require.Equal(t, http.StatusForbidden, entries[0].HTTPStatus)
	require.Contains(t, entries[0].Details, `"reason":"permission denied"`)

	h.auditLogger.FlushDenials()
	entries = attemptEntries(t, database)
	require.Len(t, entries, 2)
	require.Equal(t, 2, entries[1].EventCount)
}
