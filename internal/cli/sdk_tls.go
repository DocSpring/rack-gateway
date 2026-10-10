package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
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
//
// Gateway connections ignore HTTP(S)_PROXY / ALL_PROXY, as stdsdk's own HTTP client always did. gorilla
// would otherwise use the TLS hook to dial the proxy itself (and skip TLS to the gateway), which breaks
// logs, exec and run behind a proxy.
func secureConvoxSDK() {
	stdsdk.DefaultClient = newVerifyingSDKClient()
	websocket.DefaultDialer.NetDialTLSContext = dialVerifiedTLS
	websocket.DefaultDialer.Proxy = nil
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
	transport.Proxy = nil
	// HTTP/1.1, as with stdsdk's own client (its custom TLS config never negotiated h2).
	transport.ForceAttemptHTTP2 = false
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

// parseGatewayURL parses a gateway base URL and refuses ones the CLI must not send credentials to: plain HTTP
// to anything other than this machine, and URLs carrying user info, a query or a fragment.
func parseGatewayURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid gateway URL %q: missing host", u.Redacted())
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf(
			"invalid gateway URL %q: it must not contain user info, a query or a fragment", u.Redacted(),
		)
	}
	switch u.Scheme {
	case "https":
		return u, nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return u, nil
		}
		return nil, fmt.Errorf(
			"refusing to send credentials to %s over plain HTTP; use an https:// gateway URL", u.Host,
		)
	default:
		return nil, fmt.Errorf("unsupported gateway URL scheme %q", u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
