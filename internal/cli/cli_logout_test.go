package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeLogoutGateway answers POST /api/v1/auth/cli/logout with status, records whether it was called, and
// configures the "staging" rack to use it.
func fakeLogoutGateway(t *testing.T, status int) *bool {
	t.Helper()
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/cli/logout" ||
			r.Header.Get("Authorization") != "Bearer session-token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		called = true
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"audit log unavailable; refusing to perform logout"}`))
	}))
	t.Cleanup(server.Close)

	withCLIConfig(t, Config{Current: "staging", Gateways: map[string]GatewayConfig{
		"staging": {URL: server.URL, Token: "session-token", ExpiresAt: time.Now().Add(time.Hour)},
	}})
	return &called
}

func rackConfigured(t *testing.T, rack string) bool {
	t.Helper()
	_, found, err := configuredGateway(rack)
	require.NoError(t, err)
	return found
}

func TestLogoutEndsTheSessionThenRemovesTheRack(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		called := fakeLogoutGateway(t, status)
		require.NoError(t, logout("staging", false))
		require.True(t, *called)
		require.False(t, rackConfigured(t, "staging"), "status %d: the rack must be removed", status)
	}
}

func TestLogoutKeepsTheRackWhenTheGatewayRefuses(t *testing.T) {
	called := fakeLogoutGateway(t, http.StatusServiceUnavailable)
	err := logout("staging", false)
	require.ErrorContains(t, err, "audit log unavailable")
	require.ErrorContains(t, err, "rack-gateway logout --local --rack staging")
	require.True(t, *called)
	require.True(t, rackConfigured(t, "staging"), "a session the gateway didn't end must stay configured")
}

func TestLogoutLocalSkipsTheGateway(t *testing.T) {
	called := fakeLogoutGateway(t, http.StatusOK)
	require.NoError(t, logout("staging", true))
	require.False(t, *called)
	require.False(t, rackConfigured(t, "staging"))
}
