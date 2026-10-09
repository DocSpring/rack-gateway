package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/convox/stdsdk"
	"github.com/gorilla/websocket"
)

// sdkRootCAs overrides the trusted roots for proxied Convox requests (tests only; nil = system roots).
var sdkRootCAs *x509.CertPool

// secureConvoxSDK makes the Convox SDK verify gateway TLS certificates.
//
// stdsdk ships an http.Client and websocket dialer with InsecureSkipVerify, and it re-applies the
// insecure websocket TLS config on every websocket call. Proxied commands carry the session token and
// MFA proof in Basic auth, so a man-in-the-middle with any certificate could capture them. HTTP requests
// get a verifying client; websockets get a NetDialTLSContext hook, which gorilla uses instead of
// TLSClientConfig, so stdsdk's per-call override has no effect.
func secureConvoxSDK() {
	stdsdk.DefaultClient = newVerifyingSDKClient()
	websocket.DefaultDialer.NetDialTLSContext = dialVerifiedTLS
}

func verifyingTLSConfig(serverName string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    sdkRootCAs,
		ServerName: serverName,
	}
}

func newVerifyingSDKClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = verifyingTLSConfig("")
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.IdleConnTimeout = 90 * time.Second
	return &http.Client{Transport: transport}
}

func dialVerifiedTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 10 * time.Second},
		Config:    verifyingTLSConfig(host),
	}
	return dialer.DialContext(ctx, network, addr)
}

// requireSecureGatewayURL refuses to send credentials over plain HTTP unless the gateway is on this machine.
func requireSecureGatewayURL(gatewayURL string) error {
	u, err := url.Parse(strings.TrimSpace(gatewayURL))
	if err != nil {
		return fmt.Errorf("invalid gateway URL %q: %w", gatewayURL, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf(
			"refusing to send credentials to %s over plain HTTP; use an https:// gateway URL", u.Host,
		)
	default:
		return fmt.Errorf("unsupported gateway URL scheme %q", u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
