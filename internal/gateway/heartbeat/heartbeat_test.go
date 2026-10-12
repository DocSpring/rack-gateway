package heartbeat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEnv    = "TEST_HEARTBEAT_URL"
	monitorKey = "ping-key-0123456789"
)

// requests records the requests a monitor received.
type requests struct {
	mu    sync.Mutex
	paths []string
}

func (r *requests) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

func monitor(t *testing.T, status int) (*httptest.Server, *requests) {
	t.Helper()
	received := &requests{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.mu.Lock()
		received.paths = append(received.paths, r.Method+" "+r.URL.Path)
		received.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, received
}

func TestPingRequestsTheURL(t *testing.T) {
	server, received := monitor(t, http.StatusOK)
	pinger, err := newPinger(testEnv, server.URL+"/p/"+monitorKey+"/gateway-uptime", server.Client())
	require.NoError(t, err)

	require.NoError(t, pinger.Ping(context.Background()))
	assert.Equal(t, []string{"GET /p/" + monitorKey + "/gateway-uptime"}, received.list())
	assert.Equal(t, testEnv, pinger.Name())
}

func TestPingFailsWithoutRevealingTheURL(t *testing.T) {
	server, _ := monitor(t, http.StatusNotFound)
	pinger, err := newPinger(testEnv, server.URL+"/p/"+monitorKey+"/gateway-uptime", server.Client())
	require.NoError(t, err)

	err = pinger.Ping(context.Background())
	require.EqualError(t, err, testEnv+" ping failed: HTTP 404")

	server.Close()
	err = pinger.Ping(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), testEnv+" ping failed: ")
	assert.NotContains(t, err.Error(), monitorKey, "the URL carries the monitor's key")
}

func TestFromEnv(t *testing.T) {
	t.Setenv(testEnv, "")
	pinger, err := FromEnv(testEnv)
	require.NoError(t, err)
	assert.Nil(t, pinger, "unset means no heartbeat")

	t.Setenv(testEnv, " https://cronitor.link/p/"+monitorKey+"/gateway-uptime ")
	pinger, err = FromEnv(testEnv)
	require.NoError(t, err)
	assert.Equal(t, "https://cronitor.link/p/"+monitorKey+"/gateway-uptime", pinger.url)

	invalid := []string{"http://cronitor.link/p/" + monitorKey + "/x", "cronitor.link/p/" + monitorKey, "https://"}
	for _, value := range invalid {
		t.Setenv(testEnv, value)
		_, err = FromEnv(testEnv)
		require.EqualError(t, err, "invalid "+testEnv+": expected an https URL", value)
	}
}
