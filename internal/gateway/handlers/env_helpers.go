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
)

func (h *APIHandler) secretAndProtectedKeys(app string) ([]string, map[string]struct{}) {
	extra := make([]string, 0)
	seen := make(map[string]struct{})

	appendUniqueKeys(&extra, seen, h.secretEnvVarList(app))
	appendUniqueKeys(&extra, seen, parseEnvList(os.Getenv("CONVOX_SECRET_ENV_VARS")))

	protected := h.protectedEnvVarMap(app, seen, &extra)

	return extra, protected
}

func (h *APIHandler) secretEnvVarList(app string) []string {
	if h.settingsService == nil {
		return nil
	}
	values, err := h.settingsService.GetSecretEnvVars(app)
	if err != nil {
		return nil
	}
	return values
}

func (h *APIHandler) protectedEnvVarMap(app string, seen map[string]struct{}, extra *[]string) map[string]struct{} {
	protected := make(map[string]struct{})
	if h.settingsService == nil {
		return protected
	}

	values, err := h.settingsService.GetProtectedEnvVars(app)
	if err != nil {
		return protected
	}

	for _, value := range values {
		trim, upper, ok := normalizeEnvKey(value)
		if !ok {
			continue
		}
		protected[upper] = struct{}{}
		appendUniqueNormalized(extra, seen, trim, upper)
	}

	return protected
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

func parseEnvList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	results := make([]string, 0, len(parts))
	results = append(results, parts...)
	return results
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
	envMap, err := envutil.FetchLatestEnvMap(rackConfig, app, tlsCfg)
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
		if !canViewSecrets && envutil.IsSecretKey(key, secretKeys) {
			masked = envutil.MaskedSecret
		}
		response[key] = masked
	}
	return response
}
