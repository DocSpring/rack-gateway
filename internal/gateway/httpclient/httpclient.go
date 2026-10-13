package httpclient

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"
)

// Connection pool limits for the transports that rack requests share.
const (
	// rackIdleConnTimeout closes a pooled connection to the rack that has gone unused this long. Each one holds a TLS
	// session, buffers and two goroutines, so a burst of requests shouldn't keep its connections open for long after.
	rackIdleConnTimeout = 30 * time.Second
	// rackMaxIdleConns caps the connections kept open to the rack between requests. The gateway talks to a single
	// rack host, so this is both the per-host and the total cap. A request beyond it still gets a connection, which
	// is closed instead of pooled when the request finishes. Concurrent connections are deliberately not capped: a
	// request waiting for a free connection would let a few slow ones (an upload, a promote) hold up every other.
	rackMaxIdleConns = 16
)

// NewRackTLSConfig returns a TLS configuration that skips verification for internal Convox racks.
//
// Convox racks run on private DNS names with certs issued by the rack itself, so there is no
// trustworthy public CA chain to validate against. They still require TLS on 5443 to protect
// traffic on the internal network, and skipping verification is an intentional, documented
// trade-off for that deployment model.
func NewRackTLSConfig() *tls.Config {
	return &tls.Config{
		// #nosec G402 -- racks have no public CA chain to verify against (see above).
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
	}
}

// NewRackClient returns an HTTP client for Convox rack requests, with the given timeout. Clients made with the same
// TLS configuration share a transport, and so a pool of connections (see transportCache). Redirects are returned to
// the caller, not followed.
func NewRackClient(timeout time.Duration, tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     rackTransports.transportFor(tlsConfig),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// rackTransports holds the transports shared by every rack client in the process.
var rackTransports transportCache

// transportCache shares one transport per TLS configuration. A transport per request would leave each request's
// connection idle in a pool of its own until the idle timeout, so a client repeating a cheap request could keep
// thousands of connections (and their goroutines) open to the rack.
//
// The gateway reaches its one rack either with the default configuration (NewRackTLSConfig, asked for with a nil
// config) or with rackcert's pinned-certificate configuration, which stays the same *tls.Config until the pinned
// certificate changes. So the cache holds the default transport and the transport for the latest configuration
// it was given. A different configuration replaces that transport and closes its connections, which were verified
// against the old certificate: idle ones straight away, and ones still in use once they go idle (at the latest
// rackIdleConnTimeout after their last request).
type transportCache struct {
	mu         sync.Mutex
	defaultTr  *http.Transport
	config     *tls.Config
	configured *http.Transport
}

func (c *transportCache) transportFor(tlsConfig *tls.Config) *http.Transport {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tlsConfig == nil {
		if c.defaultTr == nil {
			c.defaultTr = newRackTransport(NewRackTLSConfig())
		}
		return c.defaultTr
	}

	if c.configured != nil && c.config == tlsConfig {
		return c.configured
	}
	if c.configured != nil {
		c.configured.CloseIdleConnections()
	}
	c.config = tlsConfig
	c.configured = newRackTransport(tlsConfig)
	return c.configured
}

func newRackTransport(tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          rackMaxIdleConns,
		MaxIdleConnsPerHost:   rackMaxIdleConns,
		IdleConnTimeout:       rackIdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}
}
