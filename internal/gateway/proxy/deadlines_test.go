package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// serverTimeout stands in for the gateway's server-wide read and write timeouts.
const serverTimeout = 500 * time.Millisecond

// serveProxyAsAdmin runs the proxy as an admin behind a server with short read and write timeouts, in front of a
// rack served by rackHandler, and returns the server's URL.
func serveProxyAsAdmin(t *testing.T, rackHandler http.HandlerFunc) string {
	t.Helper()
	url, _ := serveProxyAsAdminWithDatabase(t, rackHandler)
	return url
}

// serveProxyAsAdminWithDatabase is serveProxyAsAdmin, also returning the gateway's database.
func serveProxyAsAdminWithDatabase(t *testing.T, rackHandler http.HandlerFunc) (string, *db.Database) {
	t.Helper()
	h, database, mgr, cleanup := newProxyWithRackServer(t, rackHandler)
	t.Cleanup(cleanup)
	require.NoError(t, mgr.CreateUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	dbUser, err := database.GetUser("admin@example.com")
	require.NoError(t, err)
	admin := &auth.User{Email: dbUser.Email, Name: dbUser.Name, Roles: dbUser.Roles, DBUser: dbUser}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ProxyToRack(w, r.WithContext(context.WithValue(r.Context(), auth.UserContextKey, admin)))
	}))
	server.Config.ReadTimeout = serverTimeout
	server.Config.WriteTimeout = serverTimeout
	server.Start()
	t.Cleanup(server.Close)
	return server.URL, database
}

func requireOK(t *testing.T, resp *http.Response, err error) {
	t.Helper()
	require.NoError(t, err, "the gateway closed the connection instead of answering")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
}

// A promote can take longer than the server-wide write timeout: the rack applies the whole release before it
// answers. The client must still get the rack's response, not a closed connection.
func TestSlowPromoteOutlastsTheServerWriteTimeout(t *testing.T) {
	gateway := serveProxyAsAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			<-time.After(3 * serverTimeout) // a slow promote
		}
		_, _ = io.WriteString(w, `{"id":"R1"}`)
	})

	resp, err := http.Post(gateway+"/apps/app/releases/R1/promote", "application/x-www-form-urlencoded", nil)
	requireOK(t, resp, err)
}

// A build archive can take longer than the server-wide read timeout to arrive. The write deadline starts when the
// request's headers arrive, so it must cover reading the archive as well as sending it to the rack.
func TestSlowUploadOutlastsTheServerTimeouts(t *testing.T) {
	var received []byte
	gateway := serveProxyAsAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		answerStoredObject(w, "object://app/tmp/source.tgz")
	})

	body, writer := io.Pipe()
	go func() {
		for range 3 {
			<-time.After(serverTimeout) // a slow link
			_, _ = writer.Write([]byte("chunk"))
		}
		_ = writer.Close()
	}()
	resp, err := http.Post(gateway+"/apps/app/objects/tmp/source.tgz", "application/octet-stream", body)
	requireOK(t, resp, err)
	require.Equal(t, "chunkchunkchunk", string(received))
}

// A read the client abandons stops at the rack too, even while the rack is sending nothing: the gateway doesn't
// keep a download going for nobody until its timeout.
func TestAnAbandonedReadStopsAtTheRack(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	release := make(chan struct{})
	gateway := serveProxyAsAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-release: // the test is over: let the servers close
		}
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway+"/apps", nil)
	require.NoError(t, err)
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) // until the request is canceled
			_ = resp.Body.Close()
		}
	}()

	<-started
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the rack request kept going after its client left")
	}
}
