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
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// A promote can take longer than the server-wide write timeout: the rack applies the whole release before it
// answers. The client must still get the rack's response, not a closed connection.
func TestSlowPromoteOutlastsTheServerWriteTimeout(t *testing.T) {
	const writeTimeout = 500 * time.Millisecond
	h, database, mgr, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			<-time.After(3 * writeTimeout) // a slow promote
		}
		_, _ = io.WriteString(w, `{"id":"R1"}`)
	})
	defer cleanup()
	require.NoError(t, mgr.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	dbUser, err := database.GetUser("admin@example.com")
	require.NoError(t, err)
	admin := &auth.User{Email: dbUser.Email, Name: dbUser.Name, Roles: dbUser.Roles, DBUser: dbUser}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ProxyToRack(w, r.WithContext(context.WithValue(r.Context(), auth.UserContextKey, admin)))
	}))
	server.Config.WriteTimeout = writeTimeout
	server.Start()
	defer server.Close()

	resp, err := http.Post(server.URL+"/apps/app/releases/R1/promote", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err, "the gateway closed the connection instead of answering")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
}
