package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// recordingRack captures the requests the gateway forwards to the rack.
type recordingRack struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (rr *recordingRack) handler(w http.ResponseWriter, r *http.Request) {
	rr.mu.Lock()
	rr.requests = append(rr.requests, r.Clone(context.Background()))
	rr.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"P1"}`))
}

func (rr *recordingRack) last(t *testing.T) *http.Request {
	t.Helper()
	rr.mu.Lock()
	defer rr.mu.Unlock()
	require.NotEmpty(t, rr.requests, "expected the request to reach the rack")
	return rr.requests[len(rr.requests)-1]
}

func (rr *recordingRack) count() int {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return len(rr.requests)
}

func newRecordingProxy(t *testing.T) (*Handler, *db.Database, *recordingRack) {
	t.Helper()
	rack := &recordingRack{}
	h, database, mgr, cleanup := newProxyWithRackServer(t, rack.handler)
	t.Cleanup(cleanup)
	for userEmail, role := range map[string]string{
		"admin@test.com":    "admin",
		"ops@test.com":      "ops",
		"deployer@test.com": "deployer",
	} {
		require.NoError(t, mgr.SaveUser(userEmail, &rbac.UserConfig{Name: role, Roles: []string{role}}))
	}
	return h, database, rack
}

func withPath(req *http.Request, method, path string) *http.Request {
	clone := req.Clone(req.Context())
	clone.Method = method
	clone.URL = &url.URL{Path: path}
	clone.RequestURI = ""
	return clone
}

func TestProxyForwardsOnlyAllowlistedHeaders(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	req := withPath(requestAs(t, database, "deployer@test.com"), http.MethodGet, "/apps/myapp/processes")
	spoofed := map[string]string{
		"X-Convox-Actor":      "ceo@example.com",
		"Convox-Actor":        "ceo@example.com",
		"X-Convox-TID":        "other-tenant",
		"Convox-TID":          "other-tenant",
		"X-Forwarded-For":     "10.0.0.1",
		"Cookie":              "session_token=abc",
		"Proxy-Authorization": "Basic Zm9vOmJhcg==",
		"X-Something-Else":    "1",
		"Env":                 "SECRET=1",
	}
	for k, v := range spoofed {
		req.Header.Set(k, v)
	}
	req.Header.Set("Since", "1h")

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	forwarded := rack.last(t).Header
	require.Equal(t, "deployer@test.com", forwarded.Get("X-Convox-Actor"), "gateway must set the actor itself")
	for name := range spoofed {
		if name == "X-Convox-Actor" {
			continue
		}
		require.Emptyf(t, forwarded.Get(name), "%s must not be forwarded", name)
	}
	require.Equal(t, "1h", forwarded.Get("Since"))
	require.Equal(t, "Basic Y29udm94OnRva2Vu", forwarded.Get("Authorization"))
}

func TestCopyForwardedHeadersDropsHopByHopHeaders(t *testing.T) {
	src := http.Header{}
	for _, name := range []string{
		"Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
		"Proxy-Connection", "Accept-Encoding", "X-Forwarded-Proto", "Authorization",
	} {
		src.Set(name, "x")
	}
	src.Set("Command", "rails db:migrate")
	dst := http.Header{}
	copyForwardedHeaders(dst, src)
	require.Equal(t, http.Header{"Command": {"rails db:migrate"}}, dst)
}

func TestPrivilegedRunOptionsRequireAdmin(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	const runPath = "/apps/myapp/services/web/processes"

	admin, err := database.GetUser("admin@test.com")
	require.NoError(t, err)
	tokenID := int64(42)
	tokenUser := &auth.User{
		Email: admin.Email, IsAPIToken: true, TokenID: &tokenID, TokenName: "ci",
		Permissions: []string{"convox:*:*"}, DBUser: admin,
	}
	tokenReq := httptest.NewRequest(http.MethodPost, runPath, nil)
	tokenReq = tokenReq.WithContext(context.WithValue(tokenReq.Context(), auth.UserContextKey, tokenUser))

	callers := map[string]*http.Request{
		"ops":                  requestAs(t, database, "ops@test.com"),
		"deployer":             requestAs(t, database, "deployer@test.com"),
		"admin-owned wildcard": tokenReq,
	}
	for _, option := range []string{"Image", "Volumes", "Privileged", "Node-Labels", "Run-Tolerations"} {
		for name, base := range callers {
			req := withPath(base, http.MethodPost, runPath)
			req.Header.Set("Command", "sleep 3600")
			req.Header.Set(option, "x")
			before := rack.count()
			rr := httptest.NewRecorder()
			h.ProxyToRack(rr, req)
			require.Equalf(t, http.StatusForbidden, rr.Code, "%s with %s: %s", name, option, rr.Body.String())
			require.Equalf(t, before, rack.count(), "%s with %s must not reach the rack", name, option)
		}
	}

	req := withPath(requestAs(t, database, "admin@test.com"), http.MethodPost, runPath)
	req.Header.Set("Command", "sleep 3600")
	req.Header.Set("Image", "custom:latest")
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "custom:latest", rack.last(t).Header.Get("Image"))

	req = withPath(requestAs(t, database, "ops@test.com"), http.MethodPost, runPath)
	req.Header.Set("Command", "sleep 3600")
	req.Header.Set("Memory", "512")
	rr = httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "512", rack.last(t).Header.Get("Memory"))
}

func TestRackActorNamesTokens(t *testing.T) {
	id := int64(7)
	require.Equal(t, "me@example.com", rackActor(&auth.User{Email: "me@example.com"}))
	token := &auth.User{Email: "me@example.com", IsAPIToken: true, TokenName: "CircleCI"}
	require.Equal(t, "token:CircleCI", rackActor(token))
	require.Equal(t, "token:7", rackActor(&auth.User{IsAPIToken: true, TokenID: &id}))
}

func TestBuildTargetURLEscapesPath(t *testing.T) {
	rack := config.RackConfig{URL: "https://rack.internal:5443"}
	got, err := buildTargetURL(rack, "/apps/myapp?x/releases/R1/promote", "a=b")
	require.NoError(t, err)
	require.Equal(t, "https://rack.internal:5443/apps/myapp%3Fx/releases/R1/promote?a=b", got)

	got, err = buildTargetURL(rack, "/apps/my#app/processes", "")
	require.NoError(t, err)
	require.Equal(t, "https://rack.internal:5443/apps/my%23app/processes", got)
}

func TestIsSafeRackPath(t *testing.T) {
	require.True(t, isSafeRackPath("/apps/myapp/processes"))
	for _, path := range []string{
		"/apps/myapp?x/releases",
		"/apps/my#app/processes",
		"/apps/my\napp/processes",
		"/apps/my\x7fapp/processes",
		"/apps/../system",
		"/apps/./processes",
	} {
		require.Falsef(t, isSafeRackPath(path), "%q", path)
	}
}

func TestProxyRejectsSmuggledPathCharacters(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	req := withPath(requestAs(t, database, "admin@test.com"), http.MethodPost, "/apps/myapp?x/releases/R1/promote")
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Zero(t, rack.count())
}

func TestWebSocketDialRefusesCrossHostRedirect(t *testing.T) {
	var elsewhereHit bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		elsewhereHit = true
	}))
	defer elsewhere.Close()

	rackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", strings.Replace(elsewhere.URL, "http://", "ws://", 1)+"/steal")
		w.WriteHeader(http.StatusFound)
	}))
	defer rackServer.Close()

	wsURL, err := url.Parse(strings.Replace(rackServer.URL, "http://", "ws://", 1) + "/apps/a/processes/p/exec")
	require.NoError(t, err)
	dialer := &websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	h := &Handler{}
	conn, resp, err := h.dialWithRedirects(dialer, wsURL, http.Header{"Authorization": {"Basic secret"}})
	if conn != nil {
		_ = conn.Close()
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing websocket redirect")
	require.False(t, elsewhereHit, "rack credential must not be sent to another host")
}

func TestWebSocketDialRefusesTLSDowngradeRedirect(t *testing.T) {
	rackServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "ws://"+r.Host+"/cleartext")
		w.WriteHeader(http.StatusFound)
	}))
	defer rackServer.Close()

	wsURL, err := url.Parse(strings.Replace(rackServer.URL, "https://", "wss://", 1) + "/apps/a/processes/p/exec")
	require.NoError(t, err)
	transport, ok := rackServer.Client().Transport.(*http.Transport)
	require.True(t, ok)
	dialer := &websocket.Dialer{HandshakeTimeout: 2 * time.Second, TLSClientConfig: transport.TLSClientConfig}
	h := &Handler{}
	conn, resp, err := h.dialWithRedirects(dialer, wsURL, http.Header{"Authorization": {"Basic secret"}})
	if conn != nil {
		_ = conn.Close()
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing websocket redirect from wss://")
}
