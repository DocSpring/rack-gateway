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

// browserClient plays the browser hitting the loopback callback; it stops at the redirect so tests can
// check where the browser is sent.
var browserClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func fetch(target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := browserClient.Do(req)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	return resp, nil
}

type browserVisit struct {
	resp *http.Response
	err  error
}

// visitAsync opens target in the background, for callbacks that hold the browser until the login ends.
func visitAsync(target string) <-chan browserVisit {
	done := make(chan browserVisit, 1)
	go func() {
		resp, err := fetch(target)
		done <- browserVisit{resp: resp, err: err}
	}()
	return done
}

func awaitVisit(t *testing.T, visit <-chan browserVisit) *http.Response {
	t.Helper()
	select {
	case v := <-visit:
		require.NoError(t, v.err)
		return v.resp
	case <-time.After(5 * time.Second):
		t.Fatal("the browser was never released from the loopback callback")
		return nil
	}
}

func requireResultPage(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, want, resp.Header.Get("Location"))
	require.Equal(t, "no-referrer", resp.Header.Get("Referrer-Policy"))
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
}

const (
	testGatewayURL  = "http://127.0.0.1:9447"
	testSuccessPage = testGatewayURL + "/app/cli/auth/success"
	testErrorPage   = testGatewayURL + "/app/cli/auth/error?error="
)

func getURL(t *testing.T, target string) *http.Response {
	t.Helper()
	resp, err := fetch(target)
	require.NoError(t, err)
	return resp
}

func TestLoopbackServerListensOnLoopbackAndChecksState(t *testing.T) {
	server, err := startLoopbackServer("expected-state", testGatewayURL)
	require.NoError(t, err)
	defer server.close()

	redirect, err := url.Parse(server.redirectURI)
	require.NoError(t, err)
	require.Equal(t, "http", redirect.Scheme)
	require.Equal(t, "127.0.0.1", redirect.Hostname())
	require.Equal(t, "/callback", redirect.Path)

	// A redirect for some other login is sent to the error page and does not end the wait.
	resp := getURL(t, server.redirectURI+"?code=attacker&state=wrong-state")
	requireResultPage(t, resp, testErrorPage+"state_mismatch")
	_, err = server.wait(50 * time.Millisecond)
	require.ErrorIs(t, err, errLoginTimedOut)

	// The real redirect delivers the code, and the browser waits until the login has finished.
	visit := visitAsync(server.redirectURI + "?code=the-login-code&state=expected-state")
	code, err := server.wait(time.Second)
	require.NoError(t, err)
	require.Equal(t, "the-login-code", code)
	select {
	case <-visit:
		t.Fatal("the browser was released before the login finished")
	case <-time.After(50 * time.Millisecond):
	}

	server.finish("")
	requireResultPage(t, awaitVisit(t, visit), testSuccessPage)
}

func TestLoopbackServerSendsUnfinishedLoginToErrorPage(t *testing.T) {
	server, err := startLoopbackServer("expected-state", testGatewayURL)
	require.NoError(t, err)

	visit := visitAsync(server.redirectURI + "?code=the-login-code&state=expected-state")
	_, err = server.wait(time.Second)
	require.NoError(t, err)

	// Closing without finishing (redeeming or saving the login failed) tells the browser so.
	server.close()
	requireResultPage(t, awaitVisit(t, visit), testErrorPage+loginErrorIncomplete)
}

func TestLoopbackServerReportsGatewayError(t *testing.T) {
	server, err := startLoopbackServer("expected-state", testGatewayURL)
	require.NoError(t, err)
	defer server.close()

	resp := getURL(t, server.redirectURI+"?error=unauthorized&state=expected-state")
	requireResultPage(t, resp, testErrorPage+"unauthorized")
	_, err = server.wait(time.Second)
	require.ErrorContains(t, err, "not authorized")
}

func TestLoopbackServerReportsMissingCode(t *testing.T) {
	server, err := startLoopbackServer("expected-state", testGatewayURL)
	require.NoError(t, err)
	defer server.close()

	resp := getURL(t, server.redirectURI+"?state=expected-state")
	requireResultPage(t, resp, testErrorPage+loginErrorMissingCode)
	_, err = server.wait(time.Second)
	require.ErrorContains(t, err, "did not return a login code")
}

func TestLoopbackServerTimesOut(t *testing.T) {
	server, err := startLoopbackServer("expected-state", testGatewayURL)
	require.NoError(t, err)
	defer server.close()

	_, err = server.wait(20 * time.Millisecond)
	require.ErrorIs(t, err, errLoginTimedOut)
}

func TestLoopbackServerReportsCancelledLogin(t *testing.T) {
	server, err := startLoopbackServer("expected-state", testGatewayURL)
	require.NoError(t, err)
	defer server.close()

	resp := getURL(t, server.redirectURI+"?error=canceled&state=expected-state")
	requireResultPage(t, resp, testErrorPage+"canceled")
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
	mu             sync.Mutex
	start          LoginStartRequest
	errs           []error
	oldResponse    bool
	rejectComplete bool
	// browser receives where the browser was sent after the loopback callback.
	browser chan browserVisit
}

func newFakeLoopbackGateway() *fakeLoopbackGateway {
	return &fakeLoopbackGateway{browser: make(chan browserVisit, 1)}
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
		resp, err := fetch(start.RedirectURI + "?code=issued-code&state=" + url.QueryEscape(start.State))
		g.browser <- browserVisit{resp: resp, err: err}
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
	if g.rejectComplete || body["login_code"] != "issued-code" ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
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
	gateway := newFakeLoopbackGateway()
	server := httptest.NewServer(gateway)
	defer server.Close()
	require.NoError(t, SaveGatewayConfig("staging", server.URL))

	resp, err := runLoopbackLogin("staging", server.URL, true, "")
	require.NoError(t, err)
	require.Equal(t, "session-token", resp.Token)

	// The session is saved before the browser is shown the success page.
	cfg, _, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "session-token", cfg.Gateways["staging"].Token)
	requireResultPage(t, awaitVisit(t, gateway.browser), server.URL+"/app/cli/auth/success")

	start, errs := gateway.snapshot()
	require.Empty(t, errs)
	require.Equal(t, "S256", start.CodeChallengeMethod)
	require.Len(t, start.CodeChallenge, 43)
	require.GreaterOrEqual(t, len(start.State), 32)
	require.Regexp(t, `^http://127\.0\.0\.1:\d+/callback$`, start.RedirectURI)
}

func TestRunLoopbackLoginShowsRedeemFailureInBrowser(t *testing.T) {
	useTempConfig(t)
	gateway := newFakeLoopbackGateway()
	gateway.rejectComplete = true
	server := httptest.NewServer(gateway)
	defer server.Close()
	require.NoError(t, SaveGatewayConfig("staging", server.URL))

	_, err := runLoopbackLogin("staging", server.URL, true, "")
	require.ErrorContains(t, err, "login failed")
	requireResultPage(t, awaitVisit(t, gateway.browser),
		server.URL+"/app/cli/auth/error?error="+loginErrorIncomplete)
}

func TestRunLoopbackLoginRefusesOldGateway(t *testing.T) {
	useTempConfig(t)
	gateway := newFakeLoopbackGateway()
	gateway.oldResponse = true
	server := httptest.NewServer(gateway)
	defer server.Close()

	_, err := runLoopbackLogin("staging", server.URL, true, "")
	require.ErrorIs(t, err, errGatewayTooOld)
}
