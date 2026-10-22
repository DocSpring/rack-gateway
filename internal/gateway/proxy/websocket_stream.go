package proxy

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
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
	// messageWriteWait is how long forwarding one message may take (sessionOptions.writeWait).
	messageWriteWait = time.Minute
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

// sessionOptions are how a proxied websocket session runs.
type sessionOptions struct {
	keepalive clientKeepalive
	// writeWait bounds forwarding one message. A side that doesn't take a message in that time (a rack that never
	// reads, a client that stopped reading) ends the session, rather than holding it and its connections forever.
	writeWait time.Duration
	// clientInput says whether the client's messages go to the rack. An exec's do (they're its stdin). A log
	// stream's are read and dropped: the rack never reads them, so they'd only fill its connection.
	clientInput bool
	// recheck, when set, reports why the session must end (its caller's access is gone), or nil. It runs every
	// recheckEvery, in the background (see sessionRechecks), and one that takes longer than recheckTimeout (when
	// set) ends the session.
	recheck        func() error
	recheckEvery   time.Duration
	recheckTimeout time.Duration
	// maxLifetime, when set, ends the session that long after it started.
	maxLifetime time.Duration
}

// proxyWebSocketMessages copies messages both ways until either connection fails or closes, or the session is ended
// (see superviseSession). The caller then closes both connections, which stops the other direction.
//
// gorilla clears the server's read and write deadlines when it upgrades a connection, so without a liveness check a
// client that vanished without closing its TCP connection would hold its session, and the rack connection, forever.
// The gateway pings the client and ends the session when the client stops answering. The rack connection gets no
// such check: while it streams logs the rack never reads from its websocket, so it never answers a ping, and a
// quiet log stream would be cut. The session ends when the client's connection does, and the rack's with it.
func proxyWebSocketMessages(clientConn, upstreamConn *websocket.Conn, opts sessionOptions) {
	clientConn.SetReadLimit(maxClientMessageBytes)
	upstreamConn.SetReadLimit(maxRackMessageBytes)
	clientAlive := watchClientLiveness(clientConn, opts.keepalive.pongWait)

	var endOnce sync.Once
	end := func() {
		endOnce.Do(func() {
			_ = clientConn.Close()
			_ = upstreamConn.Close()
		})
	}
	done := make(chan struct{})
	defer close(done)
	go superviseSession(clientConn, opts, end, done)

	errc := make(chan error, 2)
	toRack := upstreamConn
	if !opts.clientInput {
		toRack = nil
	}
	go forwardMessages(clientConn, toRack, opts.writeWait, clientAlive, errc)
	go forwardMessages(upstreamConn, clientConn, opts.writeWait, func() {}, errc)
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

// superviseSession pings the client every ping period until done is closed. A failed ping is left to the read
// deadline: a client that can't be pinged stops answering, and its session ends when the deadline passes. It also
// ends the session (end closes both connections, which stops both directions) when a recheck finds its caller's
// access gone or takes too long, or once it has been open for maxLifetime.
func superviseSession(conn *websocket.Conn, opts sessionOptions, end func(), done <-chan struct{}) {
	ping := time.NewTicker(opts.keepalive.pingPeriod)
	defer ping.Stop()
	var recheck, lifetime <-chan time.Time
	if opts.recheck != nil {
		ticker := time.NewTicker(opts.recheckEvery)
		defer ticker.Stop()
		recheck = ticker.C
	}
	if opts.maxLifetime > 0 {
		timer := time.NewTimer(opts.maxLifetime)
		defer timer.Stop()
		lifetime = timer.C
	}
	checks := &sessionRechecks{check: opts.recheck, timeout: opts.recheckTimeout}
	for {
		var reason error
		select {
		case <-done:
			return
		case <-ping.C:
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(pingWriteWait))
		case <-recheck:
			checks.start()
		case reason = <-checks.result:
			checks.result, checks.deadline = nil, nil
		case <-checks.deadline:
			reason = fmt.Errorf("checking the caller's access took over %s", checks.timeout)
		case <-lifetime:
			reason = fmt.Errorf("open for %s", opts.maxLifetime)
		}
		if reason != nil {
			gtwlog.Infof("proxy: ending websocket session: %v", reason)
			end()
			return
		}
	}
}

// sessionRechecks runs a session's rechecks in the background, one at a time, so a slow one (the database
// stalled) doesn't hold up the session's pings or its lifetime limit. A recheck still running when the session
// ends finishes on its own.
type sessionRechecks struct {
	check   func() error
	timeout time.Duration
	// result receives the running recheck's answer, and deadline fires once it has taken timeout. Both are nil
	// while no recheck runs.
	result   chan error
	deadline <-chan time.Time
}

func (s *sessionRechecks) start() {
	if s.result != nil {
		return
	}
	result := make(chan error, 1)
	go func() { result <- s.check() }()
	s.result = result
	if s.timeout > 0 {
		s.deadline = time.After(s.timeout)
	}
}

// forwardMessages copies each message from src to dst (or, when dst is nil, reads and drops it), calling received
// after each one, until a read or a write fails.
func forwardMessages(src, dst *websocket.Conn, writeWait time.Duration, received func(), errc chan<- error) {
	for {
		if err := forwardMessage(src, dst, writeWait); err != nil {
			errc <- err
			return
		}
		received()
	}
}

// forwardMessage streams one message from src to dst, keeping its type, without holding it in memory: it passes
// through dst's write buffer a frame at a time, within writeWait. A message that breaks src's read limit
// fails partway. It's left unfinished, since the session is ending and finishing it would hand the other side a
// truncated message. With no dst, the message is read and dropped.
func forwardMessage(src, dst *websocket.Conn, writeWait time.Duration) error {
	messageType, r, err := src.NextReader()
	if err != nil {
		return err
	}
	if dst == nil {
		_, err := io.Copy(io.Discard, r)
		return err
	}
	if err := dst.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
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
