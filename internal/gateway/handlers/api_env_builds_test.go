package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A build the gateway checked reads the app's env when it starts on the rack, after the check, so the env editor
// waits for it to finish, as env changes through the proxy do.
func TestUpdateEnvValuesWaitsForACheckedBuild(t *testing.T) {
	var mu sync.Mutex
	buildStatus, releasesCreated := "running", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			_, _ = w.Write([]byte(`{"env":"FOO=bar"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/builds/B1":
			_, _ = w.Write([]byte(`{"id":"B1","status":"` + buildStatus + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/apps/myapp/releases":
			releasesCreated++
			_, _ = w.Write([]byte(`{"id":"R2"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, server.URL)
	require.NoError(t, rbacManager.CreateUser("deployer@example.com",
		&rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}))
	id, err := database.RecordCheckedBuild("myapp")
	require.NoError(t, err)
	require.NoError(t, database.SetCheckedBuildID(id, "B1"))

	update := func() *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]interface{}{"set": map[string]string{"FOO": "baz"}})
		require.NoError(t, err)
		c, w := newJSONContext(body)
		c.Params = gin.Params{{Key: "app", Value: "myapp"}}
		attachUser(t, c, database, "deployer@example.com", "Deployer")
		handler.UpdateEnvValues(c)
		return w
	}

	w := update()
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "build B1 is running")

	mu.Lock()
	require.Zero(t, releasesCreated)
	buildStatus = "complete"
	mu.Unlock()

	w = update()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	mu.Lock()
	require.Equal(t, 1, releasesCreated)
	mu.Unlock()
	builds, err := database.UnfinishedCheckedBuilds("myapp")
	require.NoError(t, err)
	require.Empty(t, builds, "a finished build is forgotten")
}
