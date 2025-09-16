package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	mgr, err := rbac.NewDBManager(database, "company.com")
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
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, email.NoopSender{}, "default")
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
		name       string
		path       string
		body       string
		typeExpect string
		idExpect   string
	}{
		{"app", "/apps", `{"name":"my-app"}`, "app", "my-app"},
		{"build", "/apps/my-app/builds", `{"id":"B123"}`, "build", "B123"},
		{"release", "/apps/my-app/releases", `{"id":"R456"}`, "release", "R456"},
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
			require.Equal(t, tc.idExpect, req.Header.Get("X-Audit-Resource"))
		})
	}
}

func TestForwardRequestRecordsBuildCreator(t *testing.T) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "company.com")
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
	h := NewHandler(cfg, mgr, audit.NewLogger(database), database, email.NoopSender{}, "default")

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
