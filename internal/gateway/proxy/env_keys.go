package proxy

import (
	"os"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// appEnvKeys says which of an app's env vars are secret and which are protected. It reads the app's settings once,
// so a request or response checks every key against the same lists without a query per key.
type appEnvKeys struct {
	app string
	// secretNames are CONVOX_SECRET_ENV_VARS and the app's secret_env_vars. Secret-looking names are secret too.
	secretNames []string
	protected   protectedKeySet
	// allSecret and allProtected are set when the app's setting (or CONVOX_SECRET_ENV_VARS) can't be read: every key
	// then counts as secret (or protected), so it's masked, and changed only with the matching permission.
	allSecret    bool
	allProtected bool
}

// loadSecretNames adds the names CONVOX_SECRET_ENV_VARS lists to h.secretNames. The gateway refuses to start with
// an invalid value (settings.ValidateEnvironment); a handler built without that check logs and returns the error,
// and every env var then counts as secret.
func (h *Handler) loadSecretNames() error {
	names, err := settings.SecretEnvVarNames(os.Getenv)
	if err != nil {
		gtwlog.Errorf("proxy: %v, so every env var counts as secret", err)
		return err
	}
	for _, name := range names {
		h.secretNames[name] = struct{}{}
	}
	return nil
}

// loadAppEnvKeys reads the app's secret_env_vars and protected_env_vars. Without an app, only the secret-looking
// names and CONVOX_SECRET_ENV_VARS count.
func (h *Handler) loadAppEnvKeys(app string) appEnvKeys {
	keys := appEnvKeys{
		app: app, secretNames: h.secretNameList(), protected: protectedKeySet{}, allSecret: h.secretNamesErr != nil,
	}
	if h.settingsService == nil || app == "" {
		return keys
	}
	secrets, err := h.settingsService.GetSecretEnvVars(app)
	if err != nil {
		gtwlog.Errorf("proxy: can't read secret env vars for app %q, so every key counts as secret: %v", app, err)
		keys.allSecret = true
	}
	keys.secretNames = append(keys.secretNames, secrets...)
	protected, err := h.settingsService.GetProtectedEnvVars(app)
	if err != nil {
		gtwlog.Errorf("proxy: can't read protected env vars for app %q, so every key counts as protected: %v", app, err)
		keys.allProtected = true
	}
	for _, key := range protected {
		keys.protected[strings.ToUpper(strings.TrimSpace(key))] = struct{}{}
	}
	return keys
}

// isSecret reports whether key is a secret: a secret-looking name, or one in CONVOX_SECRET_ENV_VARS or the app's
// secret_env_vars.
func (k appEnvKeys) isSecret(key string) bool {
	return k.allSecret || envutil.IsSecretKey(key, k.secretNames)
}

// masks reports whether the gateway masks key's value in env it returns: a secret, a protected key, or a value
// with credentials in it. The gateway restores a masked value posted back, so the CLI can still set other keys.
func (k appEnvKeys) masks(key, value string) bool {
	return k.isSecret(key) || k.isProtected(key) || envutil.HasCredentials(value)
}

// isProtected reports whether key is in the app's protected_env_vars.
func (k appEnvKeys) isProtected(key string) bool {
	return k.allProtected || k.protected.has(key)
}
