package cli

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/convox/convox/sdk"
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

func TestRequireSecureGatewayURL(t *testing.T) {
	for _, ok := range []string{
		"https://gateway.example.com",
		"http://localhost:8447",
		"http://127.0.0.1:9447",
		"http://[::1]:8447",
	} {
		require.NoError(t, requireSecureGatewayURL(ok), ok)
	}
	for _, bad := range []string{
		"http://gateway.example.com",
		"http://10.0.0.5:8447",
		"ftp://gateway.example.com",
	} {
		require.Error(t, requireSecureGatewayURL(bad), bad)
	}
}

func TestBuildRackURLEscapesInlineMFA(t *testing.T) {
	// Inline WebAuthn data is standard base64 and can contain '/', '+' and '='.
	auth := "SESSIONTOKEN123.webauthn.eyJzZXNzaW9uX2RhdGEiOiJ7fSJ9/+abc=="
	client, err := sdk.New(buildRackURL("https://gateway.example.com", auth))
	require.NoError(t, err)
	password, _ := client.Client.Endpoint.User.Password()
	require.Equal(t, auth, password)
	require.Equal(t, "/api/v1/rack-proxy", client.Client.Endpoint.Path)
}
