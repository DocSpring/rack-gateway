package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
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
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, settings.NewService(database), email.NoopSender{}, "default", "default", nil, nil, nil)
	return h, database, mgr
}

// pathToResourceAction converts a path and HTTP method to resource and action for RBAC
func (h *Handler) pathToResourceAction(path, method string) (string, string) {
	res, act, ok := rbac.MatchRackRoute(method, path)
	if !ok {
		return "", ""
	}
	return res.String(), act.String()
}

func TestPathToResourceActionMatchesRouteSpecs(t *testing.T) {
	h := &Handler{}

	for _, spec := range rbac.RackRouteSpecs() {
		path := rbac.RackRouteExample(spec)
		method := spec.Method
		if method == "SOCKET" {
			method = http.MethodGet
		}
		res, act := h.pathToResourceAction(path, method)
		require.Equalf(t, spec.Resource.String(), res, "pattern %s %s", spec.Method, spec.Pattern)
		require.Equalf(t, spec.Action.String(), act, "pattern %s %s", spec.Method, spec.Pattern)
	}
}

func TestAPITokenPermission_Check(t *testing.T) {
	database := dbtest.NewDatabase(t)

	// Create a mock RBAC manager
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	// Create a test user
	user, err := database.CreateUser("test@example.com", "Test User", []string{"deployer"})
	require.NoError(t, err)

	// Create an API token with specific permissions
	tokenID := int64(1)
	tokenHash := strings.Repeat("a", 64)
	permissions := []string{"convox:app:create", "convox:build:create"}
	_, err = database.CreateAPIToken(tokenHash, "test-token", user.ID, permissions, nil, nil)
	require.NoError(t, err)

	h := &Handler{rbacManager: mgr, database: database}
	u := &auth.AuthUser{
		Email:       "test@example.com",
		Permissions: permissions,
		IsAPIToken:  true,
		TokenID:     &tokenID,
	}

	// Exact match
	allowed := h.hasAPITokenPermission(u, rbac.ResourceApp, rbac.ActionCreate)
	require.True(t, allowed)

	// Not granted
	allowed = h.hasAPITokenPermission(u, rbac.ResourceApp, rbac.ActionDelete)
	require.False(t, allowed)

	// Wildcard matches - create another token with wildcard permission
	tokenID2 := int64(2)
	tokenHash2 := strings.Repeat("b", 64)
	wildcardPerms := []string{"convox:app:*"}
	_, err = database.CreateAPIToken(tokenHash2, "wildcard-token", user.ID, wildcardPerms, nil, nil)
	require.NoError(t, err)

	u2 := &auth.AuthUser{
		Email:       "test@example.com",
		Permissions: wildcardPerms,
		IsAPIToken:  true,
		TokenID:     &tokenID2,
	}
	require.True(t, h.hasAPITokenPermission(u2, rbac.ResourceApp, rbac.ActionUpdate))
	require.True(t, h.hasAPITokenPermission(u2, rbac.ResourceApp, rbac.ActionDelete))
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
		fmt.Fprint(w, `{"id":"B321","release":"R654","status":"created"}`) //nolint:errcheck
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
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, settings.NewService(database), email.NoopSender{}, "default", "default", nil, nil, nil)

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
		if log.Action == audit.BuildAction(rbac.ResourceStringRelease, rbac.ActionStringCreate) && log.Resource == "R654" {
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

	h.logEnvDiffs(req, "user@example.com", "default", []envutil.EnvDiff{{Key: "FOO", OldVal: "bar", NewVal: "", Secret: false}})

	logs, err := database.GetAuditLogs("user@example.com", time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	found := false
	for _, log := range logs {
		if log.Action == audit.BuildAction(rbac.ResourceStringEnv, audit.ActionVerbUnset) && log.Resource == "my-app/FOO" {
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

	h.logEnvDiffs(req, "user@example.com", "default", []envutil.EnvDiff{{Key: "SECRET_KEY", OldVal: "value", NewVal: "", Secret: true}})

	logs, err := database.GetAuditLogs("user@example.com", time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	found := false
	for _, log := range logs {
		if log.Action == audit.BuildAction(rbac.ResourceStringSecret, audit.ActionVerbUnset) && log.Resource == "my-app/SECRET_KEY" {
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
		fmt.Fprint(w, `{"id":"B999","status":"created"}`) //nolint:errcheck
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
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, settings.NewService(database), email.NoopSender{}, "default", "default", nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/builds", strings.NewReader(`{"foo":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Name", "Creator")

	rr := httptest.NewRecorder()
	authUser := &auth.AuthUser{
		Email: "creator@example.com",
		Name:  "Creator",
		Roles: []string{"deployer"},
	}
	status, err := h.forwardRequest(rr, req, cfg.Racks["default"], "/apps/my-app/builds", authUser)
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
