package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func newProxyForCreatorTest(t *testing.T) (*Handler, *db.Database, rbac.RBACManager) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {
			Name:     "default",
			URL:      "http://example.com",
			Username: "convox",
			APIKey:   "token",
			Enabled:  true,
		},
	}}
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, email.NoopSender{}, "default", "default")
	return h, database, mgr
}

func TestPathToResourceAction_AppRoutes(t *testing.T) {
	h := &Handler{}

	res, act := h.pathToResourceAction("/apps", "GET")
	require.Equal(t, "app", res)
	require.Equal(t, "list", act)

	res, act = h.pathToResourceAction("/apps", "POST")
	require.Equal(t, "app", res)
	require.Equal(t, "create", act)

	res, act = h.pathToResourceAction("/apps/myapp", "PUT")
	require.Equal(t, "app", res)
	require.Equal(t, "update", act)

	res, act = h.pathToResourceAction("/apps/myapp/cancel", "POST")
	require.Equal(t, "app", res)
	require.Equal(t, "update", act)

	res, act = h.pathToResourceAction("/apps/myapp", "DELETE")
	require.Equal(t, "app", res)
	require.Equal(t, "delete", act)

	// Releases create mapping
	res, act = h.pathToResourceAction("/apps/myapp/releases", "POST")
	require.Equal(t, "release", res)
	require.Equal(t, "create", act)

	// No dedicated env mapping; env is read via releases
}

func TestAPITokenPermission_Check(t *testing.T) {
	h := &Handler{}
	u := &auth.AuthUser{Permissions: []string{"convox:app:create", "convox:build:create"}, IsAPIToken: true}

	// Exact match
	allowed := h.hasAPITokenPermission(u, "app", "create")
	require.True(t, allowed)

	// Not granted
	allowed = h.hasAPITokenPermission(u, "app", "delete")
	require.False(t, allowed)

	// Wildcard matches
	u2 := &auth.AuthUser{Permissions: []string{"convox:app:*"}, IsAPIToken: true}
	require.True(t, h.hasAPITokenPermission(u2, "app", "update"))
	require.True(t, h.hasAPITokenPermission(u2, "app", "delete"))
}

func TestCaptureResourceCreatorStoresMappings(t *testing.T) {
	h, database, mgr := newProxyForCreatorTest(t)
	require.NoError(t, mgr.SaveUser("creator@example.com", &rbac.UserConfig{Name: "Creator", Roles: []string{"deployer"}}))

	cases := []struct {
		name         string
		path         string
		body         string
		typeExpect   string
		idExpect     string
		expectHeader bool
	}{
		{"app", "/apps", `{"name":"my-app"}`, "app", "my-app", true},
		{"build", "/apps/my-app/builds", `{"id":"B123","release":"R456"}`, "build", "B123", true},
		{"release", "/apps/my-app/releases", `{"id":"R456"}`, "release", "R456", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			req.Header = make(http.Header)
			h.captureResourceCreator(req, tc.path, []byte(tc.body), "creator@example.com")

			creators, err := database.GetResourceCreators(tc.typeExpect, []string{tc.idExpect})
			require.NoError(t, err)
			info := creators[tc.idExpect]
			require.NotNil(t, info)
			require.Equal(t, "creator@example.com", info.Email)
			if tc.expectHeader {
				require.Equal(t, tc.idExpect, req.Header.Get("X-Audit-Resource"))
			} else {
				require.Empty(t, req.Header.Get("X-Audit-Resource"))
			}
		})
	}
}

func TestCaptureResourceCreatorRecordsReleaseFromBuild(t *testing.T) {
	h, database, mgr := newProxyForCreatorTest(t)
	require.NoError(t, mgr.SaveUser("creator@example.com", &rbac.UserConfig{Name: "Creator", Roles: []string{"deployer"}}))

	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/builds", nil)
	req.Header = make(http.Header)

	h.captureResourceCreator(req, "/apps/my-app/builds", []byte(`{"id":"B123","release":"R456"}`), "creator@example.com")

	creators, err := database.GetResourceCreators("release", []string{"R456"})
	require.NoError(t, err)
	info := creators["R456"]
	require.NotNil(t, info)
	require.Equal(t, "creator@example.com", info.Email)
	require.ElementsMatch(t, []string{"R456"}, req.Header.Values("X-Release-Created"))

	// Subsequent GET for the same build should not enqueue duplicates
	reqGet := httptest.NewRequest(http.MethodGet, "/apps/my-app/builds/B123", nil)
	reqGet.Header = make(http.Header)
	h.captureResourceCreator(reqGet, "/apps/my-app/builds/B123", []byte(`{"id":"B123","release":"R456"}`), "creator@example.com")
	require.Empty(t, reqGet.Header.Values("X-Release-Created"))
}

func TestProxyToRackLogsReleaseAuditAndUserResource(t *testing.T) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	require.NoError(t, mgr.SaveUser("creator@example.com", &rbac.UserConfig{Name: "Creator", Roles: []string{"deployer"}}))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apps/my-app/builds", r.URL.Path)
		require.Equal(t, "Basic Y29udm94OnRva2Vu", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"B321","release":"R654","status":"created"}`)
	}))
	defer ts.Close()

	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {
			Name:     "default",
			URL:      ts.URL,
			Username: "convox",
			APIKey:   "token",
			Enabled:  true,
		},
	}}
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, email.NoopSender{}, "default", "default")

	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/builds", strings.NewReader(`{"git_sha":"abc"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Name", "Creator")
	au := &auth.AuthUser{Email: "creator@example.com", Name: "Creator"}
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, au))

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Empty(t, req.Header.Values("X-Release-Created"))

	buildCreators, err := database.GetResourceCreators("build", []string{"B321"})
	require.NoError(t, err)
	bInfo := buildCreators["B321"]
	require.NotNil(t, bInfo)
	require.Equal(t, "creator@example.com", bInfo.Email)

	releaseCreators, err := database.GetResourceCreators("release", []string{"R654"})
	require.NoError(t, err)
	rInfo := releaseCreators["R654"]
	require.NotNil(t, rInfo)
	require.Equal(t, "creator@example.com", rInfo.Email)

	logs, err := database.GetAuditLogs("creator@example.com", time.Time{}, 20)
	require.NoError(t, err)
	foundRelease := false
	for _, log := range logs {
		if log.Action == "release.create" && log.Resource == "R654" {
			foundRelease = true
			require.Equal(t, "release", log.ResourceType)
			require.Equal(t, "success", log.Status)
		}
	}
	require.True(t, foundRelease, "expected release.create audit log entry")
}

func TestLogEnvDiffsLogsUnset(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/releases", nil)
	req.Header.Set("X-User-Name", "Tester")
	req.RemoteAddr = "127.0.0.1:1234"

	h.logEnvDiffs(req, "user@example.com", "default", []EnvDiff{{Key: "FOO", OldVal: "bar", NewVal: "", Secret: false}})

	logs, err := database.GetAuditLogs("user@example.com", time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	found := false
	for _, log := range logs {
		if log.Action == "env.unset" && log.Resource == "my-app/FOO" {
			found = true
			break
		}
	}
	require.True(t, found, "expected env.unset audit log for unset env var")
}

func TestLogEnvDiffsLogsSecretUnset(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/releases", nil)
	req.Header.Set("X-User-Name", "Tester")
	req.RemoteAddr = "127.0.0.1:1234"

	h.logEnvDiffs(req, "user@example.com", "default", []EnvDiff{{Key: "SECRET_KEY", OldVal: "value", NewVal: "", Secret: true}})

	logs, err := database.GetAuditLogs("user@example.com", time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	found := false
	for _, log := range logs {
		if log.Action == "secrets.unset" && log.Resource == "my-app/SECRET_KEY" {
			found = true
			break
		}
	}
	require.True(t, found, "expected secrets.unset audit log for unset secret")
}

func TestForwardRequestRecordsBuildCreator(t *testing.T) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	require.NoError(t, mgr.SaveUser("creator@example.com", &rbac.UserConfig{Name: "Creator", Roles: []string{"deployer"}}))

	var receivedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		require.Equal(t, "/apps/my-app/builds", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"B999","status":"created"}`)
	}))
	defer ts.Close()

	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {
			Name:     "default",
			URL:      ts.URL,
			Username: "convox",
			APIKey:   "token",
			Enabled:  true,
		},
	}}
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, email.NoopSender{}, "default", "default")

	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/builds", strings.NewReader(`{"foo":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Name", "Creator")

	rr := httptest.NewRecorder()
	status, err := h.forwardRequest(rr, req, cfg.Racks["default"], "/apps/my-app/builds", "creator@example.com")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, `{"id":"B999","status":"created"}`, strings.TrimSpace(rr.Body.String()))

	creators, err := database.GetResourceCreators("build", []string{"B999"})
	require.NoError(t, err)
	info := creators["B999"]
	require.NotNil(t, info)
	require.Equal(t, "creator@example.com", info.Email)
	require.Equal(t, "B999", req.Header.Get("X-Audit-Resource"))

	// Ensure Basic auth propagated to upstream
	require.Equal(t, "Basic Y29udm94OnRva2Vu", receivedAuth)
}
