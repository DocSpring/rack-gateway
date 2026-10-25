package settings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func requiresJustification(t *testing.T, svc *Service, app string) bool {
	t.Helper()
	required, err := svc.GetRequireCommandJustification(app)
	require.NoError(t, err)
	return required
}

// TestRequireCommandJustificationIsGatewayWideOrPerApp: a command needs a justification when the gateway-wide
// setting is on or its app's own is, and an app can't opt out of the gateway-wide one.
func TestRequireCommandJustificationIsGatewayWideOrPerApp(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { _ = database.Close() })
	svc := NewService(database)

	require.False(t, requiresJustification(t, svc, "web"))

	require.NoError(t, svc.SetAppSettings("web", map[string]interface{}{KeyRequireCommandJustification: true}, nil))
	require.True(t, requiresJustification(t, svc, "web"))
	require.False(t, requiresJustification(t, svc, "worker"), "the app setting covers only its app")

	require.NoError(t, svc.SetAppSettings("web", map[string]interface{}{KeyRequireCommandJustification: false}, nil))
	require.NoError(t, svc.SetGlobalSettings(map[string]interface{}{KeyRequireCommandJustification: true}, nil))
	require.True(t, requiresJustification(t, svc, "web"), "an app turned off can't opt out")
	require.True(t, requiresJustification(t, svc, "worker"))
}

func TestRequireCommandJustificationFromTheEnvironment(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { _ = database.Close() })
	svc := NewService(database)

	t.Setenv("RGW_SETTING_REQUIRE_COMMAND_JUSTIFICATION", "true")
	require.True(t, requiresJustification(t, svc, "web"))
}

// TestRequireCommandJustificationFailsClosed: a stored gateway-wide value that isn't true or false is an error that
// requires a justification.
func TestRequireCommandJustificationFailsClosed(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { _ = database.Close() })
	svc := NewService(database)

	require.NoError(t, database.UpsertSetting(nil, KeyRequireCommandJustification, "sometimes", nil))
	required, err := svc.GetRequireCommandJustification("web")
	require.Error(t, err)
	require.True(t, required)
}
