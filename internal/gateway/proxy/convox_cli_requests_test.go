package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

func TestIsRoutineConvoxCLIRequest(t *testing.T) {
	routine := []string{"/machines", "/racks", "/apps/my-app/configs/cli-env-mask"}
	for _, path := range routine {
		require.Truef(t, isRoutineConvoxCLIRequest(http.MethodGet, path), "GET %s", path)
	}

	notRoutine := []struct{ method, path string }{
		{http.MethodPut, "/apps/my-app/configs/cli-env-mask"},
		{http.MethodPost, "/machines"},
		{"SOCKET", "/machines"},
		{http.MethodGet, "/machines/m1"},
		{http.MethodGet, "/racks/staging"},
		{http.MethodGet, "/apps/my-app/configs/other"},
		{http.MethodGet, "/apps/my-app/configs"},
		{http.MethodGet, "/apps//configs/cli-env-mask"},
		{http.MethodGet, "/apps/my-app/configs/cli-env-mask/x"},
		{http.MethodGet, "/x/apps/my-app/configs/cli-env-mask"},
	}
	for _, tc := range notRoutine {
		require.Falsef(t, isRoutineConvoxCLIRequest(tc.method, tc.path), "%s %s", tc.method, tc.path)
	}
}

// TestRoutineConvoxCLIRequestsAreNotAuditedAsDenials: the requests every Convox CLI makes for itself get the same
// 404 as any route the gateway doesn't forward, but leave no denial in the audit log, repeat counts included.
func TestRoutineConvoxCLIRequestsAreNotAuditedAsDenials(t *testing.T) {
	h, database, rack, cleanup := newAttemptProxy(t)
	defer cleanup()

	for _, path := range []string{
		"/machines", "/racks", "/apps/my-app/configs/cli-env-mask", "/convox/machines", "/machines",
	} {
		rr := proxyAs(t, h, database, http.MethodGet, path)
		require.Equalf(t, http.StatusNotFound, rr.Code, "GET %s", path)
	}
	require.Zero(t, rack.calls.Load())

	h.auditLogger.FlushDenials()
	require.Empty(t, attemptEntries(t, database))
}

// TestNearRoutineRequestsAreStillAuditedAsDenials: a request that only resembles one of the Convox CLI's own (another
// method, another config, a websocket) is refused and audited like any other route the gateway doesn't forward.
func TestNearRoutineRequestsAreStillAuditedAsDenials(t *testing.T) {
	cases := map[string]struct {
		method, path string
		websocket    bool
	}{
		"env mask write":     {method: http.MethodPut, path: "/apps/my-app/configs/cli-env-mask"},
		"another config":     {method: http.MethodGet, path: "/apps/my-app/configs/secrets"},
		"a machine":          {method: http.MethodGet, path: "/machines/m1"},
		"websocket":          {method: http.MethodGet, path: "/machines", websocket: true},
		"dot segment app":    {method: http.MethodGet, path: "/apps/../configs/cli-env-mask"},
		"query in app (%3F)": {method: http.MethodGet, path: "/apps/a?b/configs/cli-env-mask"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, database, rack, cleanup := newAttemptProxy(t)
			defer cleanup()

			req := httptest.NewRequest(tc.method, "/", nil)
			req.URL.Path = tc.path
			req = req.WithContext(requestAs(t, database, attemptUserEmail).Context())
			if tc.websocket {
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "websocket")
			}
			rr := httptest.NewRecorder()
			h.ProxyToRack(rr, req)
			require.Equal(t, http.StatusNotFound, rr.Code)
			require.Zero(t, rack.calls.Load())

			entries := attemptEntries(t, database)
			require.Len(t, entries, 1)
			require.Equal(t, audit.StatusDenied, entries[0].Status)
			require.Equal(t, http.StatusNotFound, entries[0].HTTPStatus)
			require.Contains(t, entries[0].Details, `"reason":"the gateway doesn't forward this rack route"`)
		})
	}
}
