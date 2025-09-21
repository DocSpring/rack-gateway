package httpclient

import (
	"crypto/tls"
	"net/http"
	"time"
)

// NewRackTLSConfig returns a TLS configuration that skips verification for internal Convox racks.
//
// Convox racks run on private DNS names with certs issued by the rack itself, so there is no
// trustworthy public CA chain to validate against. They still require TLS on 5443 to protect
// traffic on the internal network, and skipping verification is an intentional, documented
// trade-off for that deployment model.
func NewRackTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
	}
}

// NewRackTransport constructs a transport suitable for talking to Convox racks.
func NewRackTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       NewRackTLSConfig(),
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewRackClient returns an HTTP client preconfigured for Convox rack requests.
func NewRackClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     NewRackTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
}
