package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"crypto/tls"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
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

func (h *APIHandler) fetchEnvMap(c *gin.Context, scope, app string, rackConfig config.RackConfig, tlsCfg *tls.Config) (map[string]string, bool) {
	envMap, err := envutil.FetchLatestEnvMap(rackConfig, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			log.Printf(`{"level":"error","event":"rack_tls_verification_failed","scope":"%s","expected_fingerprint":"%s","actual_fingerprint":"%s","app":"%s"}`, scope, fpErr.Expected, fpErr.Actual, app)
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

func (h *APIHandler) logEnvUpdateDiffs(c *gin.Context, app, email, name string, diffs []envutil.EnvDiff, elapsed time.Duration) {
	if h == nil || h.database == nil || len(diffs) == 0 {
		return
	}

	ms := int(elapsed.Milliseconds())
	ip := c.ClientIP()
	ua := c.GetHeader("User-Agent")

	for _, diff := range diffs {
		oldVal := diff.OldVal
		newVal := diff.NewVal
		action := audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String())
		resourceType := "env"
		if diff.Secret {
			action = audit.BuildAction(rbac.ResourceSecret.String(), rbac.ActionSet.String())
			resourceType = "secret"
			oldVal = "[REDACTED]"
			newVal = "[REDACTED]"
		}
		if strings.TrimSpace(diff.NewVal) == "" {
			if diff.Secret {
				action = audit.BuildAction(rbac.ResourceSecret.String(), audit.ActionVerbUnset)
			} else {
				action = audit.BuildAction(rbac.ResourceEnv.String(), audit.ActionVerbUnset)
			}
		}

		detailPayload := map[string]string{"old": oldVal, "new": newVal}
		detailsJSON, _ := json.Marshal(detailPayload)

		_ = h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:      email,
			UserName:       name,
			ActionType:     "convox",
			Action:         action,
			ResourceType:   resourceType,
			Resource:       fmt.Sprintf("%s/%s", app, diff.Key),
			Details:        string(detailsJSON),
			IPAddress:      ip,
			UserAgent:      ua,
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     http.StatusOK,
			ResponseTimeMs: ms,
		})
	}
}

// GetEnvValues godoc
// @Summary Get environment variables
// @Description Returns environment variables for a Convox app, masking secrets unless authorized.
// @Tags Environment
// @Produce json
// @Param app path string true "App name"
// @Param key query string false "Specific key to fetch"
// @Param secrets query bool false "Include secret values"
// @Success 200 {object} EnvValuesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /apps/{app}/env [get]
func (h *APIHandler) GetEnvValues(c *gin.Context) {
	app := strings.TrimSpace(c.Param("app"))
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing app"})
		return
	}

	key := c.Query("key")
	wantSecrets := strings.EqualFold(c.Query("secrets"), "true")
	start := time.Now()

	email := c.GetString("user_email")
	name := c.GetString("user_name")

	if ok, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionRead); !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to view environment variables."})
		return
	}

	allowedSecrets := false
	if wantSecrets {
		if ok, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionRead); !ok {
			res := app
			if key != "" {
				res = fmt.Sprintf("%s/%s", app, key)
			}
			details := map[string]interface{}{"app": app, "secrets": true}
			if key != "" {
				details["key"] = key
			}
			detailsJSON, _ := json.Marshal(details)
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       name,
				ActionType:     "convox",
				Action:         audit.BuildAction(rbac.ResourceSecret.String(), rbac.ActionRead.String()),
				ResourceType:   "secret",
				Resource:       res,
				Details:        string(detailsJSON),
				IPAddress:      c.ClientIP(),
				UserAgent:      c.GetHeader("User-Agent"),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: int(time.Since(start).Milliseconds()),
			})
			c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to view secrets."})
			return
		}
		allowedSecrets = true
	}

	rackConfig, tlsCfg, ok := h.acquireRackContext(c)
	if !ok {
		return
	}

	envMap, ok := h.fetchEnvMap(c, "env_fetch", app, rackConfig, tlsCfg)
	if !ok {
		return
	}

	extraSecrets, _ := h.secretAndProtectedKeys(app)
	if !allowedSecrets {
		for k := range envMap {
			if envutil.IsSecretKey(k, extraSecrets) {
				envMap[k] = envutil.MaskedSecret
			}
		}
	}

	if key != "" {
		v := envMap[key]
		envMap = map[string]string{key: v}
	}

	resource := app
	if key != "" {
		resource = fmt.Sprintf("%s/%s", app, key)
	}
	details := map[string]interface{}{"app": app}
	if key != "" {
		details["key"] = key
	}
	if wantSecrets {
		details["secrets"] = true
	}
	detailsJSON, _ := json.Marshal(details)
	action := audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionRead.String())
	resourceType := "env"
	if allowedSecrets {
		action = audit.BuildAction(rbac.ResourceSecret.String(), rbac.ActionRead.String())
		resourceType = "secret"
	}
	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:      email,
		UserName:       name,
		ActionType:     "convox",
		Action:         action,
		ResourceType:   resourceType,
		Resource:       resource,
		Details:        string(detailsJSON),
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Status:         "success",
		RBACDecision:   "allow",
		HTTPStatus:     http.StatusOK,
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
	})

	c.JSON(http.StatusOK, EnvValuesResponse{Env: envMap})
}

// UpdateEnvValues godoc
// @Summary Update environment variables
// @Description Applies environment variable changes for a Convox app by creating a new release. Secrets remain masked unless the user has secrets permissions.
// @Tags Environment
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param request body UpdateEnvValuesRequest true "Environment update payload"
// @Success 200 {object} UpdateEnvValuesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/env [put]
func (h *APIHandler) UpdateEnvValues(c *gin.Context) {
	start := time.Now()

	app := strings.TrimSpace(c.Param("app"))
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app is required"})
		return
	}

	var req UpdateEnvValuesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if req.Set == nil {
		req.Set = map[string]string{}
	}
	if req.Remove == nil {
		req.Remove = []string{}
	}

	email := c.GetString("user_email")
	name := c.GetString("user_name")

	if ok, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionSet); !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to modify environment variables."})
		return
	}

	rackConfig, tlsCfg, ok := h.acquireRackContext(c)
	if !ok {
		return
	}

	baseEnv, ok := h.fetchEnvMap(c, "env_update", app, rackConfig, tlsCfg)
	if !ok {
		return
	}

	extraSecrets, protectedSet := h.secretAndProtectedKeys(app)
	allowSecrets, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionSet)
	canViewSecrets, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionRead)

	merged, diffs, mergeErr := envutil.MergeEnv(
		baseEnv,
		req.Set,
		req.Remove,
		envutil.MergeOptions{
			AllowSecretUpdates: allowSecrets,
			IsSecretKey: func(key string) bool {
				return envutil.IsSecretKey(key, extraSecrets)
			},
			IsProtectedKey: func(key string) bool {
				_, ok := protectedSet[strings.ToUpper(strings.TrimSpace(key))]
				return ok
			},
		},
	)
	if mergeErr != nil {
		switch {
		case errors.Is(mergeErr, envutil.ErrSecretPermission):
			c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to modify secrets."})
		case errors.Is(mergeErr, envutil.ErrProtectedEnvModification):
			c.JSON(http.StatusForbidden, gin.H{"error": "This environment variable is protected and cannot be changed."})
		case errors.Is(mergeErr, envutil.ErrMaskedSecretWithoutBase):
			c.JSON(http.StatusBadRequest, gin.H{"error": "Masked secret value submitted without an existing secret."})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to apply changes"})
		}
		return
	}

	if len(diffs) == 0 {
		ms := int(time.Since(start).Milliseconds())
		ip := c.ClientIP()
		ua := c.GetHeader("User-Agent")
		_ = h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:      email,
			UserName:       name,
			ActionType:     "convox",
			Action:         audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionUpdate.String()),
			ResourceType:   "env",
			Resource:       app,
			Details:        `{"changes":"none"}`,
			IPAddress:      ip,
			UserAgent:      ua,
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     http.StatusOK,
			ResponseTimeMs: ms,
		})

		c.JSON(http.StatusOK, UpdateEnvValuesResponse{Env: maskEnvForResponse(merged, extraSecrets, canViewSecrets)})
		return
	}

	envStr := envutil.BuildEnvString(merged)
	releaseID, err := envutil.CreateReleaseWithEnv(c.Request.Context(), rackConfig, tlsCfg, app, envStr)
	if err != nil {
		log.Printf(`{"level":"error","event":"env_update_release_failed","app":%q,"error":%q}`, app, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create release"})
		return
	}

	h.logEnvUpdateDiffs(c, app, email, name, diffs, time.Since(start))

	c.JSON(http.StatusOK, UpdateEnvValuesResponse{
		Env:       maskEnvForResponse(merged, extraSecrets, canViewSecrets),
		ReleaseID: releaseID,
	})
}
