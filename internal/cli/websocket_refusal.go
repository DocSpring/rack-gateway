package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// maxRecordedRefusalBytes caps how much of the gateway's answer to a refused WebSocket handshake the CLI keeps.
// gorilla reads 1 KB of a refused handshake's body, so a recording rarely gets that far.
const maxRecordedRefusalBytes = 4 << 10

// The Convox SDK (stdsdk.Client.Websocket) drops the response gorilla returns with websocket.ErrBadHandshake, so
// a refused exec, attached run or log stream would only say "websocket: bad handshake". The CLI's WebSocket
// connections record what the gateway sends until the handshake is over, and keep the response to a refused one
// here for explainRefusedHandshake.
var lastRefusedHandshake struct {
	mu       sync.Mutex
	response []byte
}

func setRefusedHandshake(response []byte) {
	lastRefusedHandshake.mu.Lock()
	defer lastRefusedHandshake.mu.Unlock()
	lastRefusedHandshake.response = append([]byte(nil), response...)
}

func takeRefusedHandshake() []byte {
	lastRefusedHandshake.mu.Lock()
	defer lastRefusedHandshake.mu.Unlock()
	response := lastRefusedHandshake.response
	lastRefusedHandshake.response = nil
	return response
}

// handshakeRecordingConn records what the server sends until the end of its handshake response. It stops at the
// headers of an accepted handshake (101 Switching Protocols), so none of the stream itself is kept, and keeps a
// refused one's response, body included, up to maxRecordedRefusalBytes.
type handshakeRecordingConn struct {
	net.Conn
	recording bool
	received  []byte
}

func recordHandshake(conn net.Conn) net.Conn {
	return &handshakeRecordingConn{Conn: conn, recording: true}
}

func (c *handshakeRecordingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if c.recording && n > 0 {
		c.record(p[:n])
	}
	return n, err
}

func (c *handshakeRecordingConn) record(data []byte) {
	if room := maxRecordedRefusalBytes - len(c.received); len(data) > room {
		data = data[:room]
	}
	c.received = append(c.received, data...)
	full := len(c.received) >= maxRecordedRefusalBytes
	if !full && !bytes.Contains(c.received, []byte("\r\n\r\n")) {
		return
	}
	if isSwitchingProtocols(c.received) {
		c.recording, c.received = false, nil
		return
	}
	setRefusedHandshake(c.received)
	if full {
		c.recording, c.received = false, nil
	}
}

func isSwitchingProtocols(response []byte) bool {
	return bytes.HasPrefix(response, []byte("HTTP/1.1 101 ")) || bytes.HasPrefix(response, []byte("HTTP/1.0 101 "))
}

// dialRecordingHandshake is the dialer for plain WebSocket connections (ws://, to a gateway on this machine).
func dialRecordingHandshake(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return recordHandshake(conn), nil
}

// handshakeRefusedError is a WebSocket handshake the gateway refused, with its reason.
type handshakeRefusedError struct {
	status int
	reason string
}

func (e *handshakeRefusedError) Error() string {
	msg := fmt.Sprintf("the gateway refused the connection (%d %s)", e.status, http.StatusText(e.status))
	if e.reason == "" {
		return msg
	}
	return msg + ": " + e.reason
}

func (*handshakeRefusedError) Unwrap() error {
	return websocket.ErrBadHandshake
}

// explainRefusedHandshake replaces websocket.ErrBadHandshake with the gateway's status and reason, when the
// connection recorded its response.
func explainRefusedHandshake(err error) error {
	if !errors.Is(err, websocket.ErrBadHandshake) {
		return err
	}
	response := takeRefusedHandshake()
	if response == nil {
		return err
	}
	resp, parseErr := http.ReadResponse(bufio.NewReader(bytes.NewReader(response)), nil)
	if parseErr != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// A body cut off at maxRecordedRefusalBytes still has its start.
	body, _ := io.ReadAll(resp.Body)
	refusal := &handshakeRefusedError{status: resp.StatusCode}
	if len(bytes.TrimSpace(body)) > 0 {
		refusal.reason = RenderGatewayError(body)
	}
	return refusal
}
