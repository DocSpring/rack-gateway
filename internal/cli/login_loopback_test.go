package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fetch(target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	return resp, nil
}

func getURL(t *testing.T, target string) *http.Response {
	t.Helper()
	resp, err := fetch(target)
	require.NoError(t, err)
	return resp
}

func TestLoopbackServerListensOnLoopbackAndChecksState(t *testing.T) {
	server, err := startLoopbackServer("expected-state", "http://127.0.0.1:9447")
	require.NoError(t, err)
	defer server.close()

	redirect, err := url.Parse(server.redirectURI)
	require.NoError(t, err)
	require.Equal(t, "http", redirect.Scheme)
	require.Equal(t, "127.0.0.1", redirect.Hostname())
	require.Equal(t, "/callback", redirect.Path)

	// A redirect for some other login is rejected and does not end the wait.
	resp := getURL(t, server.redirectURI+"?code=attacker&state=wrong-state")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_, err = server.wait(50 * time.Millisecond)
	require.ErrorIs(t, err, errLoginTimedOut)

	resp = getURL(t, server.redirectURI+"?code=the-login-code&state=expected-state")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "default-src 'none'", resp.Header.Get("Content-Security-Policy"))
	code, err := server.wait(time.Second)
	require.NoError(t, err)
	require.Equal(t, "the-login-code", code)
}

func TestLoopbackServerReportsGatewayError(t *testing.T) {
	server, err := startLoopbackServer("expected-state", "http://127.0.0.1:9447")
	require.NoError(t, err)
	defer server.close()

	getURL(t, server.redirectURI+"?error=unauthorized&state=expected-state")
	_, err = server.wait(time.Second)
	require.ErrorContains(t, err, "not authorized")
}

func TestLoopbackServerTimesOut(t *testing.T) {
	server, err := startLoopbackServer("expected-state", "http://127.0.0.1:9447")
	require.NoError(t, err)
	defer server.close()

	_, err = server.wait(20 * time.Millisecond)
	require.ErrorIs(t, err, errLoginTimedOut)
}

func TestLoopbackServerReportsCancelledLogin(t *testing.T) {
	server, err := startLoopbackServer("expected-state", "http://127.0.0.1:9447")
	require.NoError(t, err)
	defer server.close()

	getURL(t, server.redirectURI+"?error=canceled&state=expected-state")
	_, err = server.wait(time.Second)
	require.ErrorContains(t, err, "canceled in the browser")
}

func TestValidateAuthURL(t *testing.T) {
	cases := []struct {
		authURL    string
		gatewayURL string
		ok         bool
	}{
		{"https://accounts.google.com/o/oauth2/auth?x=1", "https://gateway.example.ts.net", true},
		// The gateway's identity provider can be any https OIDC issuer.
		{"https://idp.example.com/authorize?x=1", "https://gateway.example.ts.net", true},
		{"http://accounts.google.com/o/oauth2/auth", "https://gateway.example.ts.net", false},
		{"https://user:pass@idp.example.com/authorize", "https://gateway.example.ts.net", false},
		{"https:///no-host", "https://gateway.example.ts.net", false},
		{"file:///etc/passwd", "https://gateway.example.ts.net", false},
		{"http://localhost:9345/authorize", "http://127.0.0.1:9447", true},
		{"http://localhost:9345/authorize", "https://gateway.example.ts.net", false},
		{"javascript:alert(1)", "http://127.0.0.1:9447", false},
	}
	for _, tc := range cases {
		err := validateAuthURL(tc.authURL, tc.gatewayURL)
		if tc.ok {
			require.NoErrorf(t, err, "%s via %s", tc.authURL, tc.gatewayURL)
		} else {
			require.Errorf(t, err, "%s via %s", tc.authURL, tc.gatewayURL)
		}
	}
}

// fakeLoopbackGateway plays the gateway: it records the start request and only completes a login
// for the issued login code together with a verifier matching the challenge. Handler errors are
// recorded and checked by the test (require can't be called from the server goroutine).
type fakeLoopbackGateway struct {
	mu          sync.Mutex
	start       LoginStartRequest
	errs        []error
	oldResponse bool
}

func (g *fakeLoopbackGateway) fail(w http.ResponseWriter, err error) {
	g.mu.Lock()
	g.errs = append(g.errs, err)
	g.mu.Unlock()
	w.WriteHeader(http.StatusBadRequest)
}

func (g *fakeLoopbackGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/auth/cli/start":
		g.serveStart(w, r)
	case "/api/v1/auth/cli/complete":
		g.serveComplete(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (g *fakeLoopbackGateway) serveStart(w http.ResponseWriter, r *http.Request) {
	var start LoginStartRequest
	if err := json.NewDecoder(r.Body).Decode(&start); err != nil {
		g.fail(w, err)
		return
	}
	g.mu.Lock()
	g.start = start
	g.mu.Unlock()
	if g.oldResponse {
		// A gateway from before the loopback login returns its own state and verifier.
		_ = json.NewEncoder(w).Encode(map[string]string{
			"auth_url": "https://accounts.google.com/o/oauth2/auth", "state": "s", "code_verifier": "v",
		})
		return
	}
	// Play the browser: deliver the login code to the CLI's loopback listener.
	go func() {
		_, _ = fetch(start.RedirectURI + "?code=issued-code&state=" + url.QueryEscape(start.State))
	}()
	_ = json.NewEncoder(w).Encode(LoginStartResponse{AuthURL: "https://accounts.google.com/o/oauth2/auth"})
}

func (g *fakeLoopbackGateway) serveComplete(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		g.fail(w, err)
		return
	}
	g.mu.Lock()
	challenge := g.start.CodeChallenge
	g.mu.Unlock()
	sum := sha256.Sum256([]byte(body["code_verifier"]))
	if body["login_code"] != "issued-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid or expired login code"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(LoginResponse{Token: "session-token", Email: "user@example.com"})
}

func (g *fakeLoopbackGateway) snapshot() (LoginStartRequest, []error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.start, append([]error(nil), g.errs...)
}

func useTempConfig(t *testing.T) {
	t.Helper()
	previous := ConfigPath
	ConfigPath = t.TempDir()
	t.Cleanup(func() { ConfigPath = previous })
}

func TestRunLoopbackLogin(t *testing.T) {
	useTempConfig(t)
	gateway := &fakeLoopbackGateway{}
	server := httptest.NewServer(gateway)
	defer server.Close()

	resp, err := runLoopbackLogin(server.URL, true, "")
	require.NoError(t, err)
	require.Equal(t, "session-token", resp.Token)

	start, errs := gateway.snapshot()
	require.Empty(t, errs)
	require.Equal(t, "S256", start.CodeChallengeMethod)
	require.Len(t, start.CodeChallenge, 43)
	require.GreaterOrEqual(t, len(start.State), 32)
	require.Regexp(t, `^http://127\.0\.0\.1:\d+/callback$`, start.RedirectURI)
}

func TestRunLoopbackLoginRefusesOldGateway(t *testing.T) {
	useTempConfig(t)
	server := httptest.NewServer(&fakeLoopbackGateway{oldResponse: true})
	defer server.Close()

	_, err := runLoopbackLogin(server.URL, true, "")
	require.ErrorIs(t, err, errGatewayTooOld)
}
