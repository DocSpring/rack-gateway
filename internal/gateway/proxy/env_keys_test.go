package proxy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func TestAppEnvKeysClassifyAgainstTheAppSettings(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	app := "app"
	secretEnvVars := settings.AppSettingSecretEnvVars.String()
	require.NoError(t, database.UpsertSetting(&app, secretEnvVars, []string{"BILLING_ID"}, nil))
	require.NoError(t, database.UpsertSetting(&app, settings.KeyProtectedEnvVars, []string{"database_url"}, nil))

	keys := h.loadAppEnvKeys(app)
	require.True(t, keys.isSecret("billing_id"))
	require.True(t, keys.isSecret("STRIPE_SECRET"), "a secret-looking name is a secret")
	require.False(t, keys.isSecret("PORT"))
	require.True(t, keys.isProtected(" DATABASE_URL "))
	require.False(t, keys.isProtected("PORT"))

	noApp := h.loadAppEnvKeys("")
	require.False(t, noApp.isSecret("BILLING_ID"))
	require.True(t, noApp.isSecret("DATABASE_URL"), "CONVOX_SECRET_ENV_VARS counts without an app")
	require.False(t, noApp.isProtected("DATABASE_URL"))
}

// The environment endpoint returns a flat map. Without env:read every value is masked; with it, secrets and
// protected keys are.
func TestFilterEnvironmentMapResponseMasksByPermission(t *testing.T) {
	h, database, mgr := newProxyForEnvTest(t)
	app := "testapp"
	require.NoError(t, database.UpsertSetting(&app, settings.KeyProtectedEnvVars, []string{"ADMIN_URL"}, nil))
	require.NoError(t, mgr.SaveUser("viewer@test.com", &rbac.UserConfig{Name: "Viewer", Roles: []string{"viewer"}}))
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	body := []byte(`{"ADMIN_URL":"https://admin","DATABASE_URL":"postgres://db","PORT":"3000"}`)
	filtered := func(email string) map[string]string {
		out := h.filterEnvironmentMapResponse(requestAs(t, database, email), body, app)
		var env map[string]string
		require.NoError(t, json.Unmarshal(out, &env))
		return env
	}

	require.Equal(t, map[string]string{
		"ADMIN_URL": maskedSecret, "DATABASE_URL": maskedSecret, "PORT": maskedSecret,
	}, filtered("viewer@test.com"))
	require.Equal(t, map[string]string{
		"ADMIN_URL": maskedSecret, "DATABASE_URL": maskedSecret, "PORT": "3000",
	}, filtered("admin@test.com"))
}

// When the app's settings can't be read, every key counts as secret and protected: masked, and changed only with
// the matching permission.
func TestAppEnvKeysFailClosedWhenTheSettingsCantBeRead(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	require.NoError(t, database.Close())

	keys := h.loadAppEnvKeys("app")
	require.True(t, keys.isSecret("PORT"))
	require.True(t, keys.isProtected("PORT"))
	require.Equal(t, "PORT="+maskedSecret, maskSecretAndProtectedEnvVars("PORT=3000", keys))
}

// A stored list that isn't a list of env var names is an error, not an empty list, so every key counts as secret
// and protected rather than none of them.
func TestAppEnvKeysFailClosedOnMalformedStoredLists(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	app := "app"
	malformed := map[string]bool{"BILLING_ID": true}
	require.NoError(t, database.UpsertSetting(&app, settings.AppSettingSecretEnvVars.String(), malformed, nil))
	require.NoError(t, database.UpsertSetting(&app, settings.KeyProtectedEnvVars, "DATABASE_URL", nil))

	keys := h.loadAppEnvKeys(app)
	require.True(t, keys.isSecret("PORT"))
	require.True(t, keys.isProtected("PORT"))
	require.Equal(t, "PORT="+maskedSecret, maskSecretAndProtectedEnvVars("PORT=3000", keys))
}

// A value with credentials in it is masked whatever its key is called, in both the release env and the
// environment map.
func TestEnvFiltersMaskValuesWithCredentials(t *testing.T) {
	h, database, mgr := newProxyForEnvTest(t)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	keys := h.loadAppEnvKeys("testapp")

	masked := maskSecretAndProtectedEnvVars("CACHE_URL=redis://:hunter2@cache:6379\nQUEUE_URL=amqp://mq", keys)
	require.Equal(t, "CACHE_URL="+maskedSecret+"\nQUEUE_URL=amqp://mq", masked)

	body := []byte(`{"CACHE_URL":"redis://:hunter2@cache:6379","PG":"host=db password=hunter2","PORT":"3000"}`)
	out := h.filterEnvironmentMapResponse(requestAs(t, database, "admin@test.com"), body, "testapp")
	var env map[string]string
	require.NoError(t, json.Unmarshal(out, &env))
	require.Equal(t, map[string]string{"CACHE_URL": maskedSecret, "PG": maskedSecret, "PORT": "3000"}, env)
}
