package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const formContentType = "application/x-www-form-urlencoded"

// An external build is answered with a registry URL holding the rack's own API password, so it's refused for
// everyone, admins included, before it reaches the rack.
func TestExternalBuildIsRefused(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	req := withPath(requestAs(t, database, "admin@test.com"), http.MethodPost, "/apps/myapp/builds")
	req = withBody(req, "url=&external=true", formContentType)

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.NotEqual(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "external builds aren't allowed through the gateway")
	require.Zero(t, rack.count(), "an external build must not reach the rack")
}

func TestBuildRequestAcceptsOnlyKnownParameters(t *testing.T) {
	h, _, _ := newRecordingProxy(t)
	admin := &auth.User{Email: "admin@test.com"}
	cases := map[string]string{
		"url=object://myapp/tmp/a.tgz&external=false":       "external builds aren't allowed",
		"url=object://myapp/tmp/a.tgz&cache=false":          "unsupported build parameters: cache",
		"url=object://myapp/tmp/a.tgz&config=x.yml&env=A=1": "unsupported build parameters: config, env",
		"url=object://myapp/tmp/a.tgz&url=object://myapp/b": "url is given more than once",
	}
	for body, want := range cases {
		r := httptest.NewRequest(http.MethodPost, "/apps/myapp/builds", nil)
		err := h.validateBuildRequest(r, "/apps/myapp/builds", []byte(body), admin)
		require.ErrorContainsf(t, err, want, "body %q", body)
	}

	// Everything the Convox CLI sends.
	body := "url=object://myapp/tmp/a.tgz&manifest=convox.yml&description=build&git-sha=abc&build-args=A%3D1" +
		"&development=false&no-cache=true&wildcard-domain=false"
	r := httptest.NewRequest(http.MethodPost, "/apps/myapp/builds", nil)
	require.NoError(t, h.validateBuildRequest(r, "/apps/myapp/builds", []byte(body), admin))
}

// The rack joins build arguments into the build's command line and splits it like a shell, and a bare NAME takes
// its value from the build process's environment, which holds the rack's API password.
func TestBuildArgsMustBePlainNameValuePairs(t *testing.T) {
	h, _, _ := newRecordingProxy(t)
	admin := &auth.User{Email: "admin@test.com"}
	cases := map[string]string{
		"RACK_URL":                    "each must be NAME=value",
		"A=1,BUILD_AUTH":              "each must be NAME=value",
		"=1":                          "each must be NAME=value",
		"A B=1":                       "each must be NAME=value",
		"A=1 -env-wrapper true":       "invalid build argument A: its value can't contain whitespace",
		"A='x -method dir'":           "invalid build argument A",
		`A="x"`:                       "invalid build argument A",
		`A=x\ y`:                      "invalid build argument A",
		"A=x\n  image: attacker/app":  "invalid build argument A",
		"A=1,B=two words":             "invalid build argument B",
		"A=1,B=\x00":                  "invalid build argument B",
		"GOOD=1,ALSO_GOOD=a.b/c:d@e+": "",
	}
	for buildArgs, want := range cases {
		body := "url=object://myapp/tmp/a.tgz&build-args=" + queryEscape(buildArgs)
		r := httptest.NewRequest(http.MethodPost, "/apps/myapp/builds", nil)
		err := h.validateBuildRequest(r, "/apps/myapp/builds", []byte(body), admin)
		if want == "" {
			require.NoErrorf(t, err, "build-args %q", buildArgs)
			continue
		}
		require.ErrorContainsf(t, err, want, "build-args %q", buildArgs)
	}
}

func queryEscape(s string) string {
	return strings.NewReplacer("%", "%25", "&", "%26", "+", "%2B", " ", "%20", "=", "%3D", "\n", "%0A",
		"\x00", "%00", `"`, "%22", "'", "%27", `\`, "%5C").Replace(s)
}

// buildRack is a rack that answers build requests with builds carrying a repository URL, as an external build does.
type buildRack struct {
	mu       sync.Mutex
	requests []string
}

// leakedRepository is the repository URL an external build returns, carrying the rack's API password.
var leakedRepository = (&url.URL{
	Scheme: "https",
	User:   url.UserPassword("convox", "rack-api-password"),
	Host:   "api.rack.example.com",
	Path:   "/myapp",
}).String()

func (b *buildRack) handler(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.requests = append(b.requests, r.Method+" "+r.URL.Path)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	build := `{"id":"B1","status":"created","repository":"` + leakedRepository + `"}`
	if r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/builds" {
		_, _ = io.WriteString(w, `[`+build+`,{"id":"B2","Repository":"`+leakedRepository+`"},null]`)
		return
	}
	_, _ = io.WriteString(w, build)
}

// The repository field of a build can hold the rack's API password, so it's stripped from every build response.
func TestBuildResponsesNeverIncludeRepository(t *testing.T) {
	rack := &buildRack{}
	h, database, mgr, cleanup := newProxyWithRackServer(t, rack.handler)
	t.Cleanup(cleanup)
	require.NoError(t, mgr.CreateUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	requests := []struct{ method, path, body string }{
		{http.MethodPost, "/apps/myapp/builds", "url=object://myapp/tmp/source.tgz"},
		{http.MethodGet, "/apps/myapp/builds/B1", ""},
		{http.MethodGet, "/apps/myapp/builds", ""},
	}
	for _, tc := range requests {
		req := withPath(requestAs(t, database, "admin@test.com"), tc.method, tc.path)
		if tc.body != "" {
			req = withBody(req, tc.body, formContentType)
		}
		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.Equalf(t, http.StatusOK, rr.Code, "%s %s: %s", tc.method, tc.path, rr.Body.String())
		require.Containsf(t, rr.Body.String(), `"id":"B1"`, "%s %s", tc.method, tc.path)
		require.NotContainsf(t, rr.Body.String(), "rack-api-password", "%s %s", tc.method, tc.path)
		require.NotContainsf(t, strings.ToLower(rr.Body.String()), "repository", "%s %s", tc.method, tc.path)
	}
	require.Len(t, rack.requests, len(requests))
}

func TestStripBuildRepository(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"id":"B1","repository":"x"}`, `{"id":"B1"}`},
		{`[{"id":"B1","REPOSITORY":"x"},{"id":"B2"},null]`, `[{"id":"B1"},{"id":"B2"},null]`},
		{"\n{\"error\":\"not found\"}\n", `{"error":"not found"}`},
		{`"text"`, `"text"`},
		{``, ``},
	}
	for _, c := range cases {
		body, want := c.body, c.want
		got, err := stripBuildRepository([]byte(body))
		require.NoErrorf(t, err, "body %q", body)
		require.JSONEqf(t, orNull(want), orNull(string(got)), "body %q", body)
	}

	_, err := stripBuildRepository([]byte(`{"id":"B1","repository":`))
	require.Error(t, err, "a build that can't be filtered must not be passed on")
}

// orNull lets JSONEq compare empty bodies.
func orNull(s string) string {
	if strings.TrimSpace(s) == "" {
		return "null"
	}
	return s
}
