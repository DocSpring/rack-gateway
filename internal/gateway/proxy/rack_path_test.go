package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// objectRack serves one source archive and records every other request it gets.
type objectRack struct {
	t        *testing.T
	manifest string
	mu       sync.Mutex
	other    []string
}

func (o *objectRack) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/objects/tmp/source.tgz" {
		_, _ = w.Write(tarballWithManifest(o.t, o.manifest))
		return
	}
	o.mu.Lock()
	o.other = append(o.other, r.Method+" "+r.URL.Path)
	o.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"B1","status":"created"}`)
}

func (o *objectRack) forwarded() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.other...)
}

// RBAC and forwarding strip one gateway prefix from the rack path, so a build sent to
// /api/v1/rack-proxy/convox/apps/myapp/builds (which reaches the proxy as /convox/apps/myapp/builds) is
// authorized and forwarded as a build of myapp. Its manifest must be checked against myapp's patterns too.
func TestBuildThroughAnExtraPathPrefixIsValidated(t *testing.T) {
	rack := &objectRack{t: t, manifest: "services:\n  web:\n    image: attacker/app\n"}
	h, database, mgr, cleanup := newProxyWithRackServer(t, rack.handler)
	t.Cleanup(cleanup)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	app := "myapp"
	require.NoError(t, database.UpsertSetting(
		&app, "service_image_patterns", map[string]string{"*": `docker\.io/org/app:{{GIT_COMMIT}}`}, nil,
	))

	for _, path := range []string{
		"/apps/myapp/builds",
		"/convox/apps/myapp/builds",
		"/rack-proxy/apps/myapp/builds",
		"/api/v1/convox/apps/myapp/builds",
	} {
		req := withPath(requestAs(t, database, "admin@test.com"), http.MethodPost, path)
		req = withBody(req, "url=object://myapp/tmp/source.tgz&git-sha=abc", formContentType)
		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.NotEqualf(t, http.StatusOK, rr.Code, "POST %s", path)
		require.Containsf(t, rr.Body.String(), `image \"attacker/app\" does not match required pattern`,
			"POST %s", path)
	}
	require.Empty(t, rack.forwarded(), "a build with a refused manifest must not reach the rack")
}

// A path that a second normalization would change means one thing to RBAC (which normalizes again) and another to
// the rack, so it's refused without being forwarded.
func TestDoublyPrefixedRackPathIsRefused(t *testing.T) {
	rack := &objectRack{t: t}
	h, database, mgr, cleanup := newProxyWithRackServer(t, rack.handler)
	t.Cleanup(cleanup)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	for _, path := range []string{"/convox/convox/apps/myapp/builds", "/rack-proxy/convox/apps/myapp/builds"} {
		req := withPath(requestAs(t, database, "admin@test.com"), http.MethodPost, path)
		req = withBody(req, "url=object://myapp/tmp/source.tgz", formContentType)
		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.Equalf(t, http.StatusNotFound, rr.Code, "POST %s", path)
	}
	require.Empty(t, rack.forwarded())
}

func TestCanonicalRackRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/convox/apps/myapp/builds", nil)
	r.URL.RawPath = "/convox/apps/myapp/builds"
	canonical, ok := canonicalRackRequest(r)
	require.True(t, ok)
	require.Equal(t, "/apps/myapp/builds", canonical.URL.Path)
	require.Empty(t, canonical.URL.RawPath)
	require.Equal(t, "/convox/apps/myapp/builds", canonical.Header.Get("X-Original-Path"))
	require.Equal(t, "/convox/apps/myapp/builds", r.URL.Path, "the original request is left alone")

	// A path set by the CLI route is kept for the audit log.
	r = httptest.NewRequest(http.MethodGet, "/convox/apps/myapp/builds", nil)
	r.Header.Set("X-Original-Path", "/api/v1/rack-proxy/convox/apps/myapp/builds")
	canonical, ok = canonicalRackRequest(r)
	require.True(t, ok)
	require.Equal(t, "/api/v1/rack-proxy/convox/apps/myapp/builds", canonical.Header.Get("X-Original-Path"))

	r = httptest.NewRequest(http.MethodGet, "/apps/myapp/builds", nil)
	canonical, ok = canonicalRackRequest(r)
	require.True(t, ok)
	require.Same(t, r, canonical, "a rack path is used as it is")

	_, ok = canonicalRackRequest(httptest.NewRequest(http.MethodGet, "/convox/convox/apps/myapp/builds", nil))
	require.False(t, ok)
}
