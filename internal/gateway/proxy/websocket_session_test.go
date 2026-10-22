package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

// waitForSessionEnd reads from the client's connection until the gateway ends the session, failing the test if it
// doesn't within a few seconds.
func waitForSessionEnd(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			var netErr net.Error
			require.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the session was still open: %v", err)
			return
		}
	}
}

// holdRack keeps a rack connection open, without reading from it, until the test ends.
func holdRack(t *testing.T) func(*websocket.Conn) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return func(*websocket.Conn) { <-release }
}

// echoRack sends each message back.
func echoRack(conn *websocket.Conn) {
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil || conn.WriteMessage(messageType, data) != nil {
			return
		}
	}
}

// A session ends when a recheck finds its caller's access gone, and once it has been open for its maximum lifetime.
func TestWebSocketSessionEndsWhenAccessIsGoneOrItsLifetimeIsUp(t *testing.T) {
	var revoked atomic.Bool
	gateway := startWebSocketSession(t, sessionOptions{
		keepalive: defaultClientKeepalive, writeWait: messageWriteWait, clientInput: true,
		recheck: func() error {
			if revoked.Load() {
				return errSessionAccessGone
			}
			return nil
		},
		recheckEvery: 20 * time.Millisecond, recheckTimeout: time.Second,
	}, echoRack)
	conn := dialWebSocket(t, gateway)
	<-time.After(100 * time.Millisecond) // a few rechecks
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("still there?")))
	_, echo, err := conn.ReadMessage()
	require.NoError(t, err, "the session ended while access was still there")
	require.Equal(t, "still there?", string(echo))
	revoked.Store(true)
	waitForSessionEnd(t, conn)

	gateway = startWebSocketSession(t, sessionOptions{
		keepalive: defaultClientKeepalive, writeWait: messageWriteWait, clientInput: true,
		maxLifetime: 200 * time.Millisecond,
	}, holdRack(t))
	waitForSessionEnd(t, dialWebSocket(t, gateway))
}

// A recheck that hangs (the database stalled) ends the session once it has taken recheckTimeout, and doesn't hold
// up the session's lifetime limit.
func TestWebSocketSessionEndsWhenARecheckHangs(t *testing.T) {
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	stalled := func() error {
		<-hang
		return nil
	}
	gateway := startWebSocketSession(t, sessionOptions{
		keepalive: defaultClientKeepalive, writeWait: messageWriteWait, clientInput: true,
		recheck: stalled, recheckEvery: 20 * time.Millisecond, recheckTimeout: 100 * time.Millisecond,
	}, echoRack)
	waitForSessionEnd(t, dialWebSocket(t, gateway))

	gateway = startWebSocketSession(t, sessionOptions{
		keepalive: defaultClientKeepalive, writeWait: messageWriteWait, clientInput: true,
		recheck: stalled, recheckEvery: 20 * time.Millisecond, recheckTimeout: time.Hour,
		maxLifetime: 200 * time.Millisecond,
	}, holdRack(t))
	waitForSessionEnd(t, dialWebSocket(t, gateway))
}

// A log stream's rack never reads its websocket, so the client's messages are dropped rather than forwarded to
// fill its connection. An exec's are its stdin.
func TestOnlyExecSessionsForwardClientMessages(t *testing.T) {
	for _, clientInput := range []bool{false, true} {
		received := make(chan []byte, 1)
		gateway := startWebSocketSession(t, sessionOptions{
			keepalive: defaultClientKeepalive, writeWait: messageWriteWait, clientInput: clientInput,
		}, func(conn *websocket.Conn) {
			if _, data, err := conn.ReadMessage(); err == nil {
				received <- data
			}
		})
		conn := dialWebSocket(t, gateway)
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("input")))

		select {
		case data := <-received:
			require.True(t, clientInput, "a log stream forwarded the client's message")
			require.Equal(t, "input", string(data))
		case <-time.After(300 * time.Millisecond):
			require.False(t, clientInput, "an exec didn't forward the client's message")
		}
	}
}

// A rack that stops reading can't hold a session forever: once forwarding a message takes longer than the write
// wait, the session ends.
func TestABlockedWriteEndsTheSession(t *testing.T) {
	gateway := startWebSocketSession(t, sessionOptions{
		keepalive: defaultClientKeepalive, writeWait: 200 * time.Millisecond, clientInput: true,
	}, holdRack(t))
	conn := dialWebSocket(t, gateway)
	go func() {
		chunk := make([]byte, 512<<10)
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		for conn.WriteMessage(websocket.BinaryMessage, chunk) == nil {
		}
	}()
	waitForSessionEnd(t, conn)
}

// A session a deploy approval allowed ends after an hour, whether or not the approval's window has ended; any other
// lasts up to maxWebSocketSessionLifetime.
func TestAnApprovalsSessionLastsAnHourAtMost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/apps/app/processes/p1/exec", nil)
	require.Equal(t, maxWebSocketSessionLifetime, sessionLifetime(r))

	tracker := &deployApprovalTracker{request: &db.DeployApprovalRequest{ID: 1}, app: "app"}
	r = r.WithContext(context.WithValue(r.Context(), deployApprovalContextKey, tracker))
	require.Equal(t, time.Hour, sessionLifetime(r))
}

// recheck ends a session whose user is locked, whose sign-in is revoked, whose roles no longer allow it, or whose
// API token is deleted.
func TestRecheckFindsAccessThatsGone(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)
	exec := rbac.Convox(rbac.ResourceProcess, rbac.ActionExec)
	sessions := auth.NewSessionManager(database, "secret", &auth.StaticTTLProvider{TTL: time.Hour})

	signedIn := func(email, role string) sessionAccess {
		user := createUserWithRole(t, database, email, role)
		_, session, err := sessions.CreateSession(user, auth.SessionMetadata{Channel: "cli"})
		require.NoError(t, err)
		return sessionAccess{
			authUser:   &auth.User{Email: user.Email, DBUser: user, Session: session},
			permission: exec,
		}
	}

	revoked := signedIn("revoked@example.com", "admin")
	require.NoError(t, h.recheck(revoked))
	_, err := database.RevokeUserSession(revoked.authUser.Session.ID, nil)
	require.NoError(t, err)
	require.ErrorIs(t, h.recheck(revoked), errSessionAccessGone)

	locked := signedIn("locked@example.com", "admin")
	require.NoError(t, database.LockUser(locked.authUser.DBUser.ID, "test", nil))
	require.ErrorIs(t, h.recheck(locked), errSessionAccessGone)

	demoted := signedIn("demoted@example.com", "admin")
	demotion := db.UserUpdate{Roles: []string{"viewer"}, RolesWere: []string{"admin"}}
	_, err = database.UpdateUser(demoted.authUser.DBUser.ID, demotion, nil)
	require.NoError(t, err)
	require.ErrorIs(t, h.recheck(demoted), errSessionAccessGone)

	owner := createUserWithRole(t, database, "owner@example.com", "admin")
	created, err := token.NewService(database).GenerateAPIToken(&token.APITokenRequest{
		Name: "ops", UserID: owner.ID, Permissions: []string{exec},
	})
	require.NoError(t, err)
	tokenAccess := sessionAccess{
		authUser: &auth.User{
			Email: owner.Email, DBUser: owner, IsAPIToken: true, TokenID: &created.APIToken.ID,
			Permissions: created.APIToken.Permissions,
		},
		permission: exec,
	}
	require.NoError(t, h.recheck(tokenAccess))
	require.NoError(t, database.DeleteAPIToken(created.APIToken.ID))
	require.ErrorIs(t, h.recheck(tokenAccess), errSessionAccessGone)
}
