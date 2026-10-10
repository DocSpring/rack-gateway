package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

func withQuery(req *http.Request, rawQuery string) *http.Request {
	req.URL.RawQuery = rawQuery
	return req
}

// The rack reads options such as the exec command, release env and build manifest from the query string as
// well as from headers and the body, so query parameters the SDK never sends must not reach it.
func TestProxyRefusesUnknownQueryParameters(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	cases := []struct {
		method, path, query, unsupported string
	}{
		{http.MethodGet, "/apps/myapp/processes/P1/exec", "command=rm+-rf+%2F", "command"},
		{http.MethodPost, "/apps/myapp/releases", "env=SECRET_KEY%3Dabc", "env"},
		{http.MethodPost, "/apps/myapp/builds", "manifest=evil.yml&limit=1", "manifest"},
	}
	for _, tc := range cases {
		req := withQuery(withPath(requestAs(t, database, "admin@test.com"), tc.method, tc.path), tc.query)
		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.Equalf(t, http.StatusBadRequest, rr.Code, "%s %s?%s", tc.method, tc.path, tc.query)
		require.Contains(t, rr.Body.String(), "unsupported query parameters: "+tc.unsupported)
	}
	require.Zero(t, rack.count(), "refused requests must not reach the rack")
}

func TestProxyForwardsSDKQueryParameters(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	req := withQuery(
		withPath(requestAs(t, database, "deployer@test.com"), http.MethodGet, "/apps/myapp/processes"),
		"service=web&release=R1",
	)
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, url.Values{"service": {"web"}, "release": {"R1"}}, rack.last(t).URL.Query())
}

func TestDisallowedQueryParams(t *testing.T) {
	got, err := disallowedQueryParams("limit=5&command=x&Command=y&env=z")
	require.NoError(t, err)
	require.Equal(t, []string{"Command", "command", "env"}, got)

	got, err = disallowedQueryParams("")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = disallowedQueryParams("limit=%zz")
	require.Error(t, err)
}

func TestBuildWebSocketHeadersForwardsOnlyAllowlistedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/apps/myapp/processes/P1/exec", nil)
	r.Header.Set("Command", "bash")
	r.Header.Set("Height", "40")
	r.Header.Set("Tty", "true")
	r.Header.Set("Sec-WebSocket-Protocol", "convox")
	for name, value := range map[string]string{
		"X-Convox-Actor": "ceo@example.com",
		"X-Convox-TID":   "other-tenant",
		"Cookie":         "session_token=abc",
		"Authorization":  "Bearer client-session",
		"Env":            "SECRET=1",
	} {
		r.Header.Set(name, value)
	}
	rack := config.RackConfig{URL: "https://rack.internal:5443", Username: "convox", APIKey: "token"}
	wsURL, err := url.Parse("wss://rack.internal:5443/apps/myapp/processes/P1/exec")
	require.NoError(t, err)

	header := buildWebSocketHeaders(r, rack, &auth.User{Email: "ops@test.com"}, wsURL)

	require.Equal(t, "bash", header.Get("Command"))
	require.Equal(t, "40", header.Get("Height"))
	require.Equal(t, "true", header.Get("Tty"))
	require.Equal(t, "convox", header.Get("Sec-WebSocket-Protocol"))
	require.Equal(t, "ops@test.com", header.Get("X-Convox-Actor"), "gateway must set the actor itself")
	require.Equal(t, "Basic Y29udm94OnRva2Vu", header.Get("Authorization"))
	require.Equal(t, "https://rack.internal:5443", header.Get("Origin"))
	for _, name := range []string{"X-Convox-TID", "Cookie", "Env"} {
		require.Emptyf(t, header.Get(name), "%s must not be forwarded", name)
	}
}
