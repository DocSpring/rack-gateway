package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// fastKeepalive lets a test see a silent client's session end within a second or so.
var fastKeepalive = clientKeepalive{pingPeriod: 100 * time.Millisecond, pongWait: time.Second}

func websocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

// startWebSocketProxy starts a rack that runs rackSide on each websocket connection, and a gateway that proxies each
// client connection to it. It returns the gateway's websocket URL.
func startWebSocketProxy(t *testing.T, keepalive clientKeepalive, rackSide func(*websocket.Conn)) string {
	t.Helper()
	var upgrader websocket.Upgrader
	rack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		rackSide(conn)
	}))
	t.Cleanup(rack.Close)

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream, _, err := websocket.DefaultDialer.Dial(websocketURL(rack.URL), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = upstream.Close() }()
		client, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = client.Close() }()
		proxyWebSocketMessages(client, upstream, keepalive)
	}))
	t.Cleanup(gateway.Close)
	return websocketURL(gateway.URL)
}

func dialWebSocket(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// A client only sends exec input, in small messages. One message over the limit ends the session before it reaches
// the rack, rather than being read into the gateway's memory whole.
func TestWebSocketProxyRefusesAnOversizedClientMessage(t *testing.T) {
	rackRead := make(chan error, 1)
	gateway := startWebSocketProxy(t, defaultClientKeepalive, func(conn *websocket.Conn) {
		_, _, err := conn.ReadMessage()
		rackRead <- err
	})
	client := dialWebSocket(t, gateway)

	// The write fails partway once the gateway gives up on the message.
	_ = client.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("a"), maxClientMessageBytes+1))

	select {
	case err := <-rackRead:
		require.Error(t, err, "the rack must not receive a message over the client limit")
	case <-time.After(10 * time.Second):
		t.Fatal("the session should end when the client sends a message over the limit")
	}
}

// Rack output streams through: a message larger than anything a client may send arrives intact and in one piece,
// and the rack's empty binary message (Convox's end-of-stream marker) keeps its type.
func TestWebSocketProxyStreamsLargeRackOutput(t *testing.T) {
	output := bytes.Repeat([]byte("a log line\n"), (2<<20)/11)
	gateway := startWebSocketProxy(t, defaultClientKeepalive, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, output)
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{})
		_, _, _ = conn.ReadMessage() // hold the session open until the client leaves
	})
	client := dialWebSocket(t, gateway)

	messageType, message, err := client.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.True(t, bytes.Equal(output, message), "the client must receive the rack's message intact")

	messageType, message, err = client.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, messageType)
	require.Empty(t, message)
}

// gorilla clears the server's deadlines on upgrade. A client that stops answering pings (it vanished, or stopped
// reading) has its session, and its rack connection, closed instead of held forever.
func TestWebSocketProxyEndsTheSessionOfAClientThatStopsAnswering(t *testing.T) {
	rackConnClosed := make(chan struct{})
	gateway := startWebSocketProxy(t, fastKeepalive, func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage() // returns once the gateway closes the rack connection
		close(rackConnClosed)
	})
	dialWebSocket(t, gateway) // never reads, so never answers a ping

	select {
	case <-rackConnClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("the session should end once the client stops answering pings")
	}
}

type readResult struct {
	message string
	err     error
}

// A log stream can be quiet for a long time, and the rack never reads from it, so it never answers a ping. The
// session stays open as long as the client answers the gateway's pings.
func TestWebSocketProxyKeepsAQuietSessionWhileTheClientAnswers(t *testing.T) {
	speak := make(chan struct{}, 1)
	gateway := startWebSocketProxy(t, fastKeepalive, func(conn *websocket.Conn) {
		<-speak
		_ = conn.WriteMessage(websocket.TextMessage, []byte("still here\n"))
		_, _, _ = conn.ReadMessage()
	})
	t.Cleanup(func() {
		select {
		case speak <- struct{}{}:
		default:
		}
	})
	client := dialWebSocket(t, gateway)

	reads := make(chan readResult, 1)
	go func() {
		// Reading is what answers the gateway's pings.
		_, message, err := client.ReadMessage()
		reads <- readResult{message: string(message), err: err}
	}()

	select {
	case got := <-reads:
		t.Fatalf("the quiet session ended or got output early: %+v", got)
	case <-time.After(3 * fastKeepalive.pongWait):
	}

	speak <- struct{}{}
	select {
	case got := <-reads:
		require.NoError(t, got.err)
		require.Equal(t, "still here\n", got.message)
	case <-time.After(10 * time.Second):
		t.Fatal("the rack's output should reach the client")
	}
}

func TestSessionLimiterCountsEachPrincipalSeparately(t *testing.T) {
	limiter := &sessionLimiter{limit: 2}
	require.True(t, limiter.acquire("user:a"))
	require.True(t, limiter.acquire("user:a"))
	require.False(t, limiter.acquire("user:a"), "a third session is over the limit")
	require.True(t, limiter.acquire("token:1"), "another principal has its own allowance")

	limiter.release("user:a")
	require.True(t, limiter.acquire("user:a"), "a closed session frees its place")

	limiter.release("user:a")
	limiter.release("user:a")
	limiter.release("token:1")
	require.Empty(t, limiter.open)
}

func TestSessionPrincipal(t *testing.T) {
	tokenID := int64(7)
	require.Equal(t, "token:7", sessionPrincipal(&auth.User{Email: "ci@example.com", TokenID: &tokenID}))
	require.Equal(t, "user:dev@example.com", sessionPrincipal(&auth.User{Email: "Dev@example.com"}))
}

// A caller with the maximum number of exec and log sessions open is refused another before the gateway dials the
// rack.
func TestWebSocketProxyRefusesASessionOverTheCallersCap(t *testing.T) {
	authUser := &auth.User{Email: "websocket-cap@test.com"}
	principal := sessionPrincipal(authUser)
	for range maxWebSocketSessionsPerPrincipal {
		require.True(t, webSocketSessions.acquire(principal))
	}
	t.Cleanup(func() {
		for range maxWebSocketSessionsPerPrincipal {
			webSocketSessions.release(principal)
		}
	})

	var dialed atomic.Bool
	rack := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { dialed.Store(true) }))
	t.Cleanup(rack.Close)

	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/apps/myapp/logs", nil)
	rr := httptest.NewRecorder()
	status, err := h.proxyWebSocket(
		rr, req, config.RackConfig{URL: rack.URL}, rack.URL+"/apps/myapp/logs", authUser, "/apps/myapp/logs",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, http.StatusTooManyRequests, rr.Code)
	require.Contains(t, rr.Body.String(), "too many open exec and log sessions")
	require.False(t, dialed.Load(), "a refused session must not reach the rack")
}
