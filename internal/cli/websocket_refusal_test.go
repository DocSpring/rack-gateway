package cli

import (
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convox/stdsdk"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// refusingGateway answers every WebSocket handshake with status and body, as the gateway refuses an exec, an attached
// run or a log stream.
func refusingGateway(status int, contentType, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

// dialThroughSDK opens a WebSocket the way the Convox SDK does for exec and logs, and returns the error the CLI
// would print.
func dialThroughSDK(t *testing.T, gatewayURL string) error {
	t.Helper()
	secureConvoxSDK()
	client, err := stdsdk.New(gatewayURL)
	require.NoError(t, err)
	stream, err := client.Websocket("/apps/myapp/processes/web-1/exec", stdsdk.RequestOptions{})
	if err == nil {
		_ = stream.Close()
	}
	return explainRefusedHandshake(err)
}

func TestRefusedHandshakeShowsTheGatewaysReason(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        string
	}{
		{
			name:        "plain-text refusal",
			status:      http.StatusForbidden,
			contentType: "text/plain; charset=utf-8",
			body:        "permission denied: your role can't run commands in myapp\n",
			want: "the gateway refused the connection (403 Forbidden): " +
				"permission denied: your role can't run commands in myapp",
		},
		{
			name:        "JSON error",
			status:      http.StatusTooManyRequests,
			contentType: "application/json",
			body:        `{"error":"too many MFA attempts, try again in a few minutes"}`,
			want: "the gateway refused the connection (429 Too Many Requests): " +
				"too many MFA attempts, try again in a few minutes",
		},
		{
			name:        "no body",
			status:      http.StatusUnauthorized,
			contentType: "text/plain",
			want:        "the gateway refused the connection (401 Unauthorized)",
		},
		{
			name:        "control characters in the reason",
			status:      http.StatusBadRequest,
			contentType: "text/plain",
			body:        "bad \x1b[2Jrequest",
			want:        "the gateway refused the connection (400 Bad Request): bad ?[2Jrequest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(refusingGateway(tt.status, tt.contentType, tt.body))
			defer srv.Close()

			err := dialThroughSDK(t, srv.URL)
			require.EqualError(t, err, tt.want)
			require.ErrorIs(t, err, websocket.ErrBadHandshake)
			require.Nil(t, takeRefusedHandshake(), "the response is used once")
		})
	}
}

func TestRefusedHandshakeOverTLSShowsTheGatewaysReason(t *testing.T) {
	srv := httptest.NewTLSServer(refusingGateway(http.StatusForbidden, "text/plain", "MFA required\n"))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	sdkRootCAs = pool
	t.Cleanup(func() { sdkRootCAs = nil })

	require.EqualError(t, dialThroughSDK(t, srv.URL),
		"the gateway refused the connection (403 Forbidden): MFA required")
}

// An accepted handshake keeps nothing: neither its headers nor anything the stream carries.
func TestAcceptedHandshakeRecordsNothing(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("HTTP/1.1 403 Forbidden\r\n\r\nprocess output"))
		_ = conn.Close()
	}))
	defer srv.Close()
	secureConvoxSDK()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(message), "process output")
	require.NoError(t, conn.Close())
	require.Nil(t, takeRefusedHandshake())
}

// The response can arrive in pieces, and a large body is cut off at maxRecordedRefusalBytes.
func TestHandshakeRecordingConnAcrossReadsAndAtTheLimit(t *testing.T) {
	read := func(response string) {
		server, client := net.Pipe()
		go func() {
			for len(response) > 0 {
				n := min(7, len(response))
				_, _ = server.Write([]byte(response[:n]))
				response = response[n:]
			}
			_ = server.Close()
		}()
		conn := recordHandshake(client)
		_, _ = io.Copy(io.Discard, conn)
		_ = conn.Close()
	}

	read("HTTP/1.1 403 Forbidden\r\nContent-Length: 13\r\n\r\nnot your app\n")
	require.EqualError(t, explainRefusedHandshake(websocket.ErrBadHandshake),
		"the gateway refused the connection (403 Forbidden): not your app")

	long := strings.Repeat("x", 2*maxRecordedRefusalBytes)
	read("HTTP/1.1 400 Bad Request\r\nContent-Length: 8192\r\n\r\n" + long)
	err := explainRefusedHandshake(websocket.ErrBadHandshake)
	require.ErrorContains(t, err, "the gateway refused the connection (400 Bad Request): xxx")
	require.Less(t, len(err.Error()), maxRecordedRefusalBytes+64)

	read("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n\r\nHTTP/1.1 403 Forbidden\r\n\r\nstream")
	require.Nil(t, takeRefusedHandshake())
}

func TestExplainRefusedHandshakeLeavesOtherErrorsAlone(t *testing.T) {
	other := errors.New("connection refused")
	require.Equal(t, other, explainRefusedHandshake(other))
	require.NoError(t, explainRefusedHandshake(nil))
	require.Nil(t, takeRefusedHandshake())
	require.Equal(t, websocket.ErrBadHandshake, explainRefusedHandshake(websocket.ErrBadHandshake),
		"without a recorded response, the SDK's error stands")

	setRefusedHandshake([]byte("not an HTTP response"))
	require.Equal(t, websocket.ErrBadHandshake, explainRefusedHandshake(websocket.ErrBadHandshake))
}
