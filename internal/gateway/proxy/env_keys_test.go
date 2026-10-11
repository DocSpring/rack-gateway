package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func TestAppEnvKeysClassifyAgainstTheAppSettings(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	app := "app"
	require.NoError(t, database.UpsertSetting(&app, settings.KeySecretEnvVars, []string{"BILLING_ID"}, nil))
	require.NoError(t, database.UpsertSetting(&app, settings.KeyProtectedEnvVars, []string{"database_url"}, nil))

	keys := h.loadAppEnvKeys(app)
	require.True(t, keys.isSecret("billing_id"))
	require.True(t, keys.isSecret("STRIPE_SECRET"), "a secret-looking name is a secret")
	require.False(t, keys.isSecret("PORT"))
	require.True(t, keys.isProtected(" DATABASE_URL "))
	require.False(t, keys.isProtected("PORT"))

	noApp := h.loadAppEnvKeys("")
	require.False(t, noApp.isSecret("BILLING_ID"))
	require.False(t, noApp.isProtected("DATABASE_URL"))
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
