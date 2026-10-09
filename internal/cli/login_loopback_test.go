package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestValidateAuthURL(t *testing.T) {
	cases := []struct {
		authURL    string
		gatewayURL string
		ok         bool
	}{
		{"https://accounts.google.com/o/oauth2/auth?x=1", "https://gateway.example.ts.net", true},
		{"http://accounts.google.com/o/oauth2/auth", "https://gateway.example.ts.net", false},
		{"https://evil.example/login", "https://gateway.example.ts.net", false},
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
// for the issued login code together with a verifier matching the challenge.
type fakeLoopbackGateway struct {
	t     *testing.T
	start LoginStartRequest
}

func (g *fakeLoopbackGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/auth/cli/start":
		require.NoError(g.t, json.NewDecoder(r.Body).Decode(&g.start))
		// Play the browser: deliver the login code to the CLI's loopback listener.
		go func() {
			_, _ = fetch(g.start.RedirectURI + "?code=issued-code&state=" + url.QueryEscape(g.start.State))
		}()
		_ = json.NewEncoder(w).Encode(LoginStartResponse{AuthURL: "https://accounts.google.com/o/oauth2/auth"})
	case "/api/v1/auth/cli/complete":
		var body map[string]string
		require.NoError(g.t, json.NewDecoder(r.Body).Decode(&body))
		sum := sha256.Sum256([]byte(body["code_verifier"]))
		if body["login_code"] != "issued-code" ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != g.start.CodeChallenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid or expired login code"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(LoginResponse{Token: "session-token", Email: "user@example.com"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestRunLoopbackLogin(t *testing.T) {
	ConfigPath = t.TempDir()
	gateway := &fakeLoopbackGateway{t: t}
	server := httptest.NewServer(gateway)
	defer server.Close()

	resp, err := runLoopbackLogin(server.URL, true, "")
	require.NoError(t, err)
	require.Equal(t, "session-token", resp.Token)

	require.Equal(t, "S256", gateway.start.CodeChallengeMethod)
	require.Len(t, gateway.start.CodeChallenge, 43)
	require.GreaterOrEqual(t, len(gateway.start.State), 32)
	require.Regexp(t, `^http://127\.0\.0\.1:\d+/callback$`, gateway.start.RedirectURI)
}
