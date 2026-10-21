package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// connectionURL is a database URL with a password in it.
func connectionURL(host, password string) string {
	return (&url.URL{Scheme: "postgres", User: url.UserPassword("app", password), Host: host, Path: "/app"}).String()
}

// envRack is a rack whose latest release has a DATABASE_URL with a password in it, and which records the env of
// the release the gateway creates.
type envRack struct {
	mu     sync.Mutex
	posted string
}

func (rk *envRack) createdEnv() string {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	return rk.posted
}

func fakeEnvRack(t *testing.T) (*httptest.Server, *envRack) {
	t.Helper()
	rack := &envRack{}
	env := "DATABASE_URL=" + connectionURL("db", "hunter2") + "\nFOO=bar\nLOG_LEVEL=info"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			data, _ := json.Marshal(map[string]string{"env": env})
			_, _ = w.Write(data)
		case r.Method == http.MethodPost && r.URL.Path == "/apps/myapp/releases":
			body, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(body))
			rack.mu.Lock()
			rack.posted = values.Get("env")
			rack.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"R2"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, rack
}

// newDeployerEnvHandler is an API handler in front of the rack at rackURL, with a deployer (env:set, but no
// secret:read) to make the changes.
func newDeployerEnvHandler(t *testing.T, rackURL string) (*handlers.APIHandler, *db.Database) {
	t.Helper()
	// The dev environment marks DATABASE_URL secret by name.
	t.Setenv("CONVOX_SECRET_ENV_VARS", "")
	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, rackURL)
	cfg := &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}
	require.NoError(t, rbacManager.CreateUser("deployer@example.com", cfg))
	return handler, database
}

func putEnvAsDeployer(
	ctx context.Context,
	t *testing.T,
	handler *handlers.APIHandler,
	database *db.Database,
	payload handlers.UpdateEnvValuesRequest,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	c, w := newJSONContext(body)
	c.Request = c.Request.WithContext(ctx)
	c.Params = gin.Params{{Key: "app", Value: "myapp"}}
	attachUser(t, c, database, "deployer@example.com", "Deployer User")
	handler.UpdateEnvValues(c)
	return w
}

func updateEnvAsDeployer(
	t *testing.T,
	rackURL string,
	payload handlers.UpdateEnvValuesRequest,
) (handlers.UpdateEnvValuesResponse, *db.Database) {
	t.Helper()
	handler, database := newDeployerEnvHandler(t, rackURL)
	w := putEnvAsDeployer(context.Background(), t, handler, database, payload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp handlers.UpdateEnvValuesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp, database
}

// While another env change to the app holds its env lock, an update gives up with 409 rather than writing back
// an env the other change is about to replace.
func TestUpdateEnvValuesRefusesWhileAnotherChangeIsInProgress(t *testing.T) {
	server, rack := fakeEnvRack(t)
	handler, database := newDeployerEnvHandler(t, server.URL)
	held, err := database.LockAppEnv(context.Background(), "myapp")
	require.NoError(t, err)
	defer held.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	w := putEnvAsDeployer(ctx, t, handler, database, handlers.UpdateEnvValuesRequest{
		Set: map[string]string{"FOO": "baz"},
	})

	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Empty(t, rack.createdEnv())

	held.Release()
	w = putEnvAsDeployer(context.Background(), t, handler, database, handlers.UpdateEnvValuesRequest{
		Set: map[string]string{"FOO": "baz"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, rack.createdEnv(), "FOO=baz")
}

// DATABASE_URL isn't secret by name, but a deployer can't read secrets, so its value is masked because it has
// credentials in it. Posting the mask back keeps the value.
func TestUpdateEnvValuesMasksValuesWithCredentials(t *testing.T) {
	server, rack := fakeEnvRack(t)

	resp, _ := updateEnvAsDeployer(t, server.URL, handlers.UpdateEnvValuesRequest{
		Set: map[string]string{"FOO": "baz", "DATABASE_URL": envutil.MaskedSecret},
	})

	require.Equal(t, envutil.MaskedSecret, resp.Env["DATABASE_URL"])
	require.Equal(t, "baz", resp.Env["FOO"])
	require.Contains(t, rack.createdEnv(), "DATABASE_URL="+connectionURL("db", "hunter2"))
	require.Contains(t, rack.createdEnv(), "FOO=baz")
}

// A deployer has env:set, so they can change a connection URL they can't read.
func TestUpdateEnvValuesLetsEnvSettersReplaceAMaskedURL(t *testing.T) {
	server, rack := fakeEnvRack(t)

	updateEnvAsDeployer(t, server.URL, handlers.UpdateEnvValuesRequest{
		Set: map[string]string{"DATABASE_URL": connectionURL("db2", "new")},
	})

	require.Contains(t, rack.createdEnv(), "DATABASE_URL="+connectionURL("db2", "new"))
}

// Setting a key to an empty or blank value is a set, not an unset: only a removed key is audited as env.unset.
func TestUpdateEnvValuesAuditsEmptyValuesAsSets(t *testing.T) {
	server, rack := fakeEnvRack(t)

	_, database := updateEnvAsDeployer(t, server.URL, handlers.UpdateEnvValuesRequest{
		Set:    map[string]string{"FOO": " "},
		Remove: []string{"LOG_LEVEL"},
	})

	require.Contains(t, rack.createdEnv(), "FOO= ")
	require.NotContains(t, rack.createdEnv(), "LOG_LEVEL")
	logs, err := database.GetAuditLogs("deployer@example.com", time.Time{}, 20)
	require.NoError(t, err)
	actions := map[string]string{}
	for _, entry := range logs {
		actions[entry.Resource] = entry.Action
	}
	require.Equal(t, audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()), actions["myapp/FOO"])
	require.Equal(t, audit.BuildAction(rbac.ResourceEnv.String(), audit.ActionVerbUnset), actions["myapp/LOG_LEVEL"])
}
