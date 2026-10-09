package cli

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/convox/convox/sdk"
	"github.com/convox/stdsdk"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type authRecorder struct {
	mu   sync.Mutex
	auth []string
}

func (a *authRecorder) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.auth = append(a.auth, r.Header.Get("Authorization"))
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

func (a *authRecorder) seen() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.auth...)
}

func configureRackForTLSTest(t *testing.T, gatewayURL string) {
	t.Helper()
	ConfigPath = t.TempDir()
	RackFlag = ""
	t.Setenv("RACK_GATEWAY_API_TOKEN", "")
	t.Setenv("RACK_GATEWAY_URL", "")
	require.NoError(t, SaveConfig(&Config{
		Current: "us",
		Gateways: map[string]GatewayConfig{
			"us": {URL: gatewayURL, Token: "SESSION-TOKEN-SECRET", ExpiresAt: time.Now().Add(time.Hour)},
		},
	}))
}

func TestConvoxCommandsRejectUntrustedTLSCertificate(t *testing.T) {
	recorder := &authRecorder{}
	srv := httptest.NewTLSServer(http.HandlerFunc(recorder.handler))
	defer srv.Close()
	configureRackForTLSTest(t, srv.URL)

	client, _, err := SetupConvoxCommandWithMFA(&cobra.Command{}, nil, "totp.123456")
	require.NoError(t, err)
	_, err = client.AppList()
	require.Error(t, err)
	require.Contains(t, err.Error(), "certificate")
	require.Empty(t, recorder.seen(), "credentials must not reach a server with an untrusted certificate")
}

func TestConvoxCommandsAcceptTrustedTLSCertificate(t *testing.T) {
	recorder := &authRecorder{}
	srv := httptest.NewTLSServer(http.HandlerFunc(recorder.handler))
	defer srv.Close()
	configureRackForTLSTest(t, srv.URL)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	sdkRootCAs = pool
	t.Cleanup(func() { sdkRootCAs = nil })

	client, _, err := SetupConvoxCommandWithMFA(&cobra.Command{}, nil, "totp.123456")
	require.NoError(t, err)
	_, err = client.AppList()
	require.NoError(t, err)
	require.Len(t, recorder.seen(), 1)
}

func TestWebsocketTLSDialVerifiesCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "https://")

	_, err := dialVerifiedTLS(context.Background(), "tcp", addr)
	require.Error(t, err, "untrusted certificate must be rejected")

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	sdkRootCAs = pool
	t.Cleanup(func() { sdkRootCAs = nil })

	conn, err := dialVerifiedTLS(context.Background(), "tcp", addr)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

func TestParseGatewayURL(t *testing.T) {
	for _, ok := range []string{
		"https://gateway.example.com",
		"https://gateway.example.com/base",
		"http://localhost:8447",
		"http://127.0.0.1:9447",
		"http://[::1]:8447",
	} {
		_, err := parseGatewayURL(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{
		"http://gateway.example.com",
		"http://10.0.0.5:8447",
		"ftp://gateway.example.com",
		"https://gateway.example.com?x=1",
		"https://gateway.example.com?",
		"https://gateway.example.com#frag",
		"https://user:pass@gateway.example.com",
		"https://",
	} {
		_, err := parseGatewayURL(bad)
		require.Error(t, err, bad)
	}
}

// Every CLI request carries the session token, so the plain-HTTP refusal applies wherever the gateway URL is
// loaded (MFA calls and native API calls too), not only when the Convox SDK client is built.
func TestNormalizeGatewayURLRefusesPlainHTTP(t *testing.T) {
	_, err := NormalizeGatewayURL("http://10.0.0.5:8447")
	require.ErrorContains(t, err, "plain HTTP")

	normalized, err := NormalizeGatewayURL("gateway.example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example.com", normalized)

	normalized, err = NormalizeGatewayURL("http://127.0.0.1:8447")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8447", normalized)
}

func TestBuildRackURLNeverEchoesTheToken(t *testing.T) {
	const token = "SESSION-TOKEN-SECRET"
	for _, gatewayURL := range []string{
		"https://gw.example.com?x=1",
		"https://gw.example.com#frag",
		"https://u:p@gw.example.com",
		"http://gw.example.com",
		"https://gw.example.com/%zz",
	} {
		_, err := buildRackURL(gatewayURL, token+".totp.123456")
		require.Error(t, err, gatewayURL)
		require.NotContains(t, err.Error(), token, gatewayURL)
	}

	rackURL, err := buildRackURL("https://gateway.example.com/base/", token)
	require.NoError(t, err)
	client, err := sdk.New(rackURL)
	require.NoError(t, err)
	require.Equal(t, "/base/api/v1/rack-proxy", client.Client.Endpoint.Path)
}

// The Convox SDK sends Endpoint.User.String() as the Basic credential. Check the exact bytes a realistic inline
// WebAuthn proof (base64 of the JSON the CLI builds) arrives as.
func TestInlineWebAuthnProofReachesGatewayUnchanged(t *testing.T) {
	recorder := &authRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer srv.Close()
	configureRackForTLSTest(t, srv.URL)

	payload, err := json.Marshal(map[string]string{
		"session_data": "wac_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJ",
		"assertion_response": `{"id":"cred-id","rawId":"cred-id","type":"public-key","response":{` +
			`"authenticatorData":"SZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2MFAAAAAQ",` +
			`"clientDataJSON":"eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0","signature":"MEUCIQDx-sig_value"}}`,
	})
	require.NoError(t, err)
	mfaAuth := "webauthn." + base64.StdEncoding.EncodeToString(payload)

	client, _, err := SetupConvoxCommandWithMFA(&cobra.Command{}, nil, mfaAuth)
	require.NoError(t, err)
	_, err = client.AppList()
	require.NoError(t, err)

	seen := recorder.seen()
	require.Len(t, seen, 1)
	credential, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(seen[0], "Basic "))
	require.NoError(t, err)
	require.Equal(t, "convox:SESSION-TOKEN-SECRET."+mfaAuth, string(credential))
}

// Gateway websocket connections ignore proxy settings (as stdsdk's HTTP client always did) and still verify TLS.
func TestWebsocketDialIgnoresProxyAndVerifiesTLS(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	wsURL := "wss://" + strings.TrimPrefix(srv.URL, "https://")

	unreachableProxy, err := url.Parse("http://127.0.0.1:1")
	require.NoError(t, err)
	websocket.DefaultDialer.Proxy = http.ProxyURL(unreachableProxy)
	t.Cleanup(func() { websocket.DefaultDialer.Proxy = http.ProxyFromEnvironment })
	secureConvoxSDK()
	require.Nil(t, websocket.DefaultDialer.Proxy)
	transport, ok := stdsdk.DefaultClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy)
	require.False(t, transport.ForceAttemptHTTP2)

	_, _, err = websocket.DefaultDialer.Dial(wsURL, nil)
	require.Error(t, err, "untrusted certificate must be rejected")
	require.Contains(t, err.Error(), "certificate")

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	sdkRootCAs = pool
	t.Cleanup(func() { sdkRootCAs = nil })
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "the dial goes straight to the gateway, not the proxy")
	require.NoError(t, conn.Close())
}
