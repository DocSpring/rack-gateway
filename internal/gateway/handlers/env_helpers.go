package handlers

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// secretAndProtectedKeys returns the app's secret env vars (its secret_env_vars and CONVOX_SECRET_ENV_VARS) and
// protected ones. It fails when the app's settings can't be read: guessing would unmask or unprotect keys.
func (h *APIHandler) secretAndProtectedKeys(app string) ([]string, map[string]struct{}, error) {
	extra := make([]string, 0)
	seen := make(map[string]struct{})

	secrets, err := h.secretEnvVarList(app)
	if err != nil {
		return nil, nil, err
	}
	appendUniqueKeys(&extra, seen, secrets)
	convoxSecrets, err := settings.SecretEnvVarNames(os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	appendUniqueKeys(&extra, seen, convoxSecrets)

	protected, err := h.protectedEnvVarMap(app, seen, &extra)
	if err != nil {
		return nil, nil, err
	}
	return extra, protected, nil
}

// respondEnvSettingsError answers 500 when the app's env var settings (or CONVOX_SECRET_ENV_VARS) can't be read,
// and logs why: guessing would unmask or unprotect keys, and the cause, such as an invalid name in a stored list,
// is something an admin has to fix.
func respondEnvSettingsError(c *gin.Context, app string, err error) {
	log.Printf(`{"level":"error","event":"env_settings_failed","app":%q,"error":%q}`, app, err.Error())
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the app's env var settings"})
}

// auditVisibleKeys returns the env vars whose values the app's env change audit entries may show. When the
// app's settings can't be read, none are shown.
func (h *APIHandler) auditVisibleKeys(app string) envutil.AuditVisibleKeys {
	if h.settingsService == nil {
		return envutil.AuditVisibleKeys{}
	}
	keys := envutil.AuditVisibleKeys{}
	secretNames, err := settings.SecretEnvVarNames(os.Getenv)
	if err == nil {
		keys, err = envutil.LoadAuditVisibleKeys(h.settingsService, app, secretNames)
	}
	if err != nil {
		log.Printf(`{"level":"error","event":"audit_env_settings_failed","app":%q,"error":%q}`, app, err.Error())
	}
	return keys
}

func (h *APIHandler) secretEnvVarList(app string) ([]string, error) {
	if h.settingsService == nil {
		return nil, nil
	}
	return h.settingsService.GetSecretEnvVars(app)
}

func (h *APIHandler) protectedEnvVarMap(
	app string, seen map[string]struct{}, extra *[]string,
) (map[string]struct{}, error) {
	protected := make(map[string]struct{})
	if h.settingsService == nil {
		return protected, nil
	}

	values, err := h.settingsService.GetProtectedEnvVars(app)
	if err != nil {
		return nil, err
	}

	for _, value := range values {
		trim, upper, ok := normalizeEnvKey(value)
		if !ok {
			continue
		}
		protected[upper] = struct{}{}
		appendUniqueNormalized(extra, seen, trim, upper)
	}

	return protected, nil
}

func appendUniqueKeys(extra *[]string, seen map[string]struct{}, values []string) {
	for _, value := range values {
		trim, upper, ok := normalizeEnvKey(value)
		if !ok {
			continue
		}
		appendUniqueNormalized(extra, seen, trim, upper)
	}
}

func appendUniqueNormalized(extra *[]string, seen map[string]struct{}, trim, upper string) {
	if _, exists := seen[upper]; exists {
		return
	}
	seen[upper] = struct{}{}
	*extra = append(*extra, trim)
}

func normalizeEnvKey(value string) (string, string, bool) {
	trim := strings.TrimSpace(value)
	if trim == "" {
		return "", "", false
	}
	upper := strings.ToUpper(trim)
	return trim, upper, true
}

func (_ *APIHandler) fetchEnvMap(
	c *gin.Context,
	scope, app string,
	rackConfig config.RackConfig,
	tlsCfg *tls.Config,
) (map[string]string, bool) {
	envMap, err := envutil.FetchLatestEnvMap(c.Request.Context(), rackConfig, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			log.Printf(
				`{"level":"error","event":"rack_tls_verification_failed",`+
					`"scope":"%s","expected_fingerprint":"%s","actual_fingerprint":"%s","app":"%s"}`,
				scope,
				fpErr.Expected,
				fpErr.Actual,
				app,
			)
			c.JSON(http.StatusBadGateway, gin.H{"error": "rack certificate verification failed"})
			return nil, false
		}

		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch env"})
		return nil, false
	}

	if envMap == nil {
		return map[string]string{}, true
	}

	return envMap, true
}

func maskEnvForResponse(values map[string]string, secretKeys []string, canViewSecrets bool) map[string]string {
	response := make(map[string]string, len(values))
	for key, value := range values {
		masked := value
		if !canViewSecrets && envutil.MaskedForReaders(key, value, secretKeys) {
			masked = envutil.MaskedSecret
		}
		response[key] = masked
	}
	return response
}
