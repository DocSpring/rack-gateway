package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Test that a deployer cannot delete an app via the proxy (403 expected).
func TestDeployerCannotDeleteApp(t *testing.T) {
	// Setup DB with a deployer user
	database := dbtest.NewDatabase(t)
	_, err := database.CreateUser("deployer@test.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)

	// RBAC manager (DB)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	// Minimal config with a default rack
	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {
			Name:     "default",
			URL:      "http://mock",
			Username: "convox",
			APIKey:   "token",
			Enabled:  true,
		},
	}}

	h := NewHandler(
		cfg,
		mgr,
		audit.NewLogger(database),
		database,
		settings.NewService(database),
		email.NoopSender{},
		"testrack",
		"testrack",
		nil,
		nil,
		nil,
	)

	// Create request: DELETE /apps/myapp
	req := httptest.NewRequest(http.MethodDelete, "/apps/myapp", nil)
	// Inject authenticated session user into context
	au := &auth.AuthUser{Email: "deployer@test.com", Name: "Deployer", IsAPIToken: false}
	ctx := context.WithValue(req.Context(), auth.UserContextKey, au)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
}
