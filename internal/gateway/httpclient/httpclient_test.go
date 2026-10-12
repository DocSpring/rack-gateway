package httpclient

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTLSRack starts a TLS server and counts the connections it has seen closed.
func newTLSRack(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var closed atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, &closed
}

// trusting returns a new TLS config that trusts the server's certificate, as a distinct pinned config would.
func trusting(t *testing.T, server *httptest.Server) *tls.Config {
	t.Helper()
	transport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	return transport.TLSClientConfig.Clone()
}

// get makes a request and reports whether it went over a pooled connection.
func get(t *testing.T, client *http.Client, url string) bool {
	t.Helper()
	var reused bool
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}
	req, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, url, nil,
	)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return reused
}

// Each proxied request makes its own client. They must share connections, or every request would leave another
// idle connection (and its goroutines) open to the rack.
func TestRackClientsWithTheSameTLSConfigShareConnections(t *testing.T) {
	server, _ := newTLSRack(t)
	pinned := trusting(t, server)

	first := NewRackClient(5*time.Second, pinned)
	unpinned := NewRackClient(5*time.Second, nil)
	second := NewRackClient(30*time.Second, pinned)

	require.Same(t, first.Transport, second.Transport, "a client without a config must not displace the pinned pool")
	require.NotSame(t, first.Transport, unpinned.Transport)
	require.Equal(t, 30*time.Second, second.Timeout, "each client keeps its own timeout")
	require.False(t, get(t, first, server.URL))
	require.True(t, get(t, second, server.URL), "the second client should reuse the first one's connection")
}

// A new pinned config (the rack certificate was refreshed) gets a new pool, and the old pool's connections, verified
// against the old certificate, are closed rather than left open until they time out.
func TestRackClientForANewTLSConfigClosesTheOldConnections(t *testing.T) {
	server, closed := newTLSRack(t)

	old := NewRackClient(5*time.Second, trusting(t, server))
	get(t, old, server.URL)
	require.Zero(t, closed.Load())

	refreshed := NewRackClient(5*time.Second, trusting(t, server))
	require.NotSame(t, old.Transport, refreshed.Transport)
	require.Eventually(t, func() bool { return closed.Load() >= 1 }, 5*time.Second, 10*time.Millisecond,
		"the replaced transport's idle connection should be closed")
	require.False(t, get(t, refreshed, server.URL), "a new config must not use a connection from the old pool")
}

func TestRackClientsWithoutATLSConfigShareTheDefaultTransport(t *testing.T) {
	first := NewRackClient(time.Second, nil)
	second := NewRackClient(time.Second, nil)
	require.Same(t, first.Transport, second.Transport)

	transport, ok := first.Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	require.Equal(t, []string{"http/1.1"}, transport.TLSClientConfig.NextProtos)
	require.Equal(t, rackIdleConnTimeout, transport.IdleConnTimeout)
	require.Equal(t, rackMaxIdleConns, transport.MaxIdleConnsPerHost)
}

func TestRackClientReturnsRedirectsWithoutFollowingThem(t *testing.T) {
	rack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	t.Cleanup(rack.Close)

	resp, err := NewRackClient(5*time.Second, nil).Get(rack.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusFound, resp.StatusCode)
}
