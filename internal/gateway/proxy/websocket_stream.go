package proxy

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
)

// Message size limits for a proxied websocket session (an exec or a log stream).
const (
	// maxClientMessageBytes caps one message from the client. A client only sends exec input, which the Convox SDK
	// (stdsdk) sends as it reads it, at most 1 KiB per message, so no real message comes close.
	maxClientMessageBytes = 1 << 20
	// maxRackMessageBytes caps one message from the rack. The rack writes output (logs, exec) as it copies it, at
	// most 32 KiB per message (io.Copy's buffer). Messages are streamed through, not held in memory, so this is only
	// a backstop against a runaway message and can be generous.
	maxRackMessageBytes = 16 << 20
	// pingWriteWait bounds writing one ping to the client.
	pingWriteWait = 10 * time.Second
)

// clientKeepalive is how the gateway checks that a websocket client is still there.
type clientKeepalive struct {
	// pingPeriod is how often the gateway pings the client.
	pingPeriod time.Duration
	// pongWait is how long the client may go without a sign of life before its session ends. It's well over
	// pingPeriod, so a live client's pong arrives in time even when it's queued behind a backlog of output.
	pongWait time.Duration
}

var defaultClientKeepalive = clientKeepalive{pingPeriod: 30 * time.Second, pongWait: 60 * time.Second}

// proxyWebSocketMessages copies messages both ways until either connection fails or closes. The caller then closes
// both connections, which stops the other direction.
//
// gorilla clears the server's read and write deadlines when it upgrades a connection, so without a liveness check a
// client that vanished without closing its TCP connection would hold its session, and the rack connection, forever.
// The gateway pings the client and ends the session when the client stops answering. The rack connection gets no
// such check: while it streams logs the rack never reads from its websocket, so it never answers a ping, and a
// quiet log stream would be cut. The session ends when the client's connection does, and the rack's with it.
func proxyWebSocketMessages(clientConn, upstreamConn *websocket.Conn, keepalive clientKeepalive) {
	clientConn.SetReadLimit(maxClientMessageBytes)
	upstreamConn.SetReadLimit(maxRackMessageBytes)
	clientAlive := watchClientLiveness(clientConn, keepalive.pongWait)

	done := make(chan struct{})
	defer close(done)
	go pingClient(clientConn, keepalive.pingPeriod, done)

	errc := make(chan error, 2)
	go forwardMessages(clientConn, upstreamConn, clientAlive, errc)
	go forwardMessages(upstreamConn, clientConn, func() {}, errc)
	<-errc
}

// watchClientLiveness gives the client pongWait to show a sign of life, and extends that whenever it does: on a pong
// (an answer to the gateway's ping), on a ping of its own (the Convox SDK, stdsdk, pings every 5 seconds), and on each
// message. A message counts because the gateway may have spent a while forwarding the previous one to the rack
// instead of reading the client's pongs. It returns the function that extends the deadline after a message.
func watchClientLiveness(conn *websocket.Conn, pongWait time.Duration) func() {
	extend := func() {
		// An error means the connection is already broken, which the next read reports.
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	}
	extend()

	conn.SetPongHandler(func(string) error {
		extend()
		return nil
	})
	answerPing := conn.PingHandler()
	conn.SetPingHandler(func(appData string) error {
		extend()
		return answerPing(appData)
	})
	return extend
}

// pingClient pings the client every period until done is closed. A failed ping is left to the read deadline: a
// client that can't be pinged stops answering, and its session ends when the deadline passes.
func pingClient(conn *websocket.Conn, period time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(pingWriteWait))
		}
	}
}

// forwardMessages copies each message from src to dst, calling received after each one, until a read or a write
// fails.
func forwardMessages(src, dst *websocket.Conn, received func(), errc chan<- error) {
	for {
		if err := forwardMessage(src, dst); err != nil {
			errc <- err
			return
		}
		received()
	}
}

// forwardMessage streams one message from src to dst, keeping its type, without holding it in memory: it passes
// through dst's write buffer a frame at a time. A message that breaks src's read limit fails partway. It's left
// unfinished, since the session is ending and finishing it would hand the other side a truncated message.
func forwardMessage(src, dst *websocket.Conn) error {
	messageType, r, err := src.NextReader()
	if err != nil {
		return err
	}
	w, err := dst.NextWriter(messageType)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	return w.Close()
}
