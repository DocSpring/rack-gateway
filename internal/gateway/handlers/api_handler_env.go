package handlers

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *APIHandler) logEnvUpdateDiffs(
	c *gin.Context,
	app, email, name string,
	diffs []envutil.EnvDiff,
	elapsed time.Duration,
) {
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

	allowedSecrets, ok := h.checkSecretPermissions(c, email, name, app, key, wantSecrets, start)
	if !ok {
		return
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
	h.maskSecretsIfNeeded(envMap, extraSecrets, allowedSecrets)

	if key != "" {
		v := envMap[key]
		envMap = map[string]string{key: v}
	}

	h.logEnvRead(c, email, name, app, key, wantSecrets, allowedSecrets, start)

	c.JSON(http.StatusOK, EnvValuesResponse{Env: envMap})
}

func (h *APIHandler) checkSecretPermissions(
	c *gin.Context,
	email, name, app, key string,
	wantSecrets bool,
	start time.Time,
) (bool, bool) {
	if !wantSecrets {
		return false, true
	}

	if ok, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionRead); ok {
		return true, true
	}

	h.logSecretAccessDenied(c, email, name, app, key, start)
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to view secrets."})
	return false, false
}

func (h *APIHandler) logSecretAccessDenied(
	c *gin.Context,
	email, name, app, key string,
	start time.Time,
) {
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
}

func (h *APIHandler) maskSecretsIfNeeded(envMap map[string]string, secretKeys []string, allowed bool) {
	if allowed {
		return
	}
	for k := range envMap {
		if envutil.IsSecretKey(k, secretKeys) {
			envMap[k] = envutil.MaskedSecret
		}
	}
}

func (h *APIHandler) logEnvRead(
	c *gin.Context,
	email, name, app, key string,
	wantSecrets, allowedSecrets bool,
	start time.Time,
) {
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
}

// UpdateEnvValues godoc
// @Summary Update environment variables
// @Description Applies environment variable changes for a Convox app by creating a new release.
// @Description Secrets remain masked unless the user has secrets permissions.
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
	ctx, ok := h.prepareEnvUpdateContext(c)
	if !ok {
		return
	}

	merged, diffs, err := h.mergeEnvChanges(ctx)
	if err != nil {
		h.respondMergeError(c, err)
		return
	}

	if len(diffs) == 0 {
		h.respondNoEnvChanges(c, ctx, merged)
		return
	}

	releaseID, err := envutil.CreateReleaseWithEnv(
		c.Request.Context(),
		ctx.rackConfig,
		ctx.tlsCfg,
		ctx.app,
		envutil.BuildEnvString(merged),
	)
	if err != nil {
		log.Printf(`{"level":"error","event":"env_update_release_failed","app":%q,"error":%q}`, ctx.app, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create release"})
		return
	}

	h.logEnvUpdateDiffs(c, ctx.app, ctx.email, ctx.name, diffs, time.Since(ctx.start))

	c.JSON(http.StatusOK, UpdateEnvValuesResponse{
		Env:       maskEnvForResponse(merged, ctx.extraSecrets, ctx.canViewSecrets),
		ReleaseID: releaseID,
	})
}

type envUpdateContext struct {
	app            string
	request        UpdateEnvValuesRequest
	email          string
	name           string
	allowSecrets   bool
	canViewSecrets bool
	extraSecrets   []string
	protectedKeys  map[string]struct{}
	baseEnv        map[string]string
	rackConfig     config.RackConfig
	tlsCfg         *tls.Config
	start          time.Time
}

func (h *APIHandler) prepareEnvUpdateContext(c *gin.Context) (*envUpdateContext, bool) {
	ctx := &envUpdateContext{start: time.Now()}
	ctx.app = strings.TrimSpace(c.Param("app"))
	if ctx.app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app is required"})
		return nil, false
	}

	if err := c.ShouldBindJSON(&ctx.request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return nil, false
	}
	if ctx.request.Set == nil {
		ctx.request.Set = map[string]string{}
	}
	if ctx.request.Remove == nil {
		ctx.request.Remove = []string{}
	}

	ctx.email = c.GetString("user_email")
	ctx.name = c.GetString("user_name")

	if ok, _ := h.rbac.Enforce(ctx.email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionSet); !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to modify environment variables."})
		return nil, false
	}

	rackConfig, tlsCfg, ok := h.acquireRackContext(c)
	if !ok {
		return nil, false
	}
	ctx.rackConfig = rackConfig
	ctx.tlsCfg = tlsCfg

	baseEnv, ok := h.fetchEnvMap(c, "env_update", ctx.app, rackConfig, tlsCfg)
	if !ok {
		return nil, false
	}
	ctx.baseEnv = baseEnv

	ctx.extraSecrets, ctx.protectedKeys = h.secretAndProtectedKeys(ctx.app)
	ctx.allowSecrets, _ = h.rbac.Enforce(ctx.email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionSet)
	ctx.canViewSecrets, _ = h.rbac.Enforce(ctx.email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionRead)

	return ctx, true
}

func (h *APIHandler) mergeEnvChanges(ctx *envUpdateContext) (map[string]string, []envutil.EnvDiff, error) {
	return envutil.MergeEnv(
		ctx.baseEnv,
		ctx.request.Set,
		ctx.request.Remove,
		envutil.MergeOptions{
			AllowSecretUpdates: ctx.allowSecrets,
			IsSecretKey: func(key string) bool {
				return envutil.IsSecretKey(key, ctx.extraSecrets)
			},
			IsProtectedKey: func(key string) bool {
				_, ok := ctx.protectedKeys[strings.ToUpper(strings.TrimSpace(key))]
				return ok
			},
		},
	)
}

func (h *APIHandler) respondMergeError(c *gin.Context, mergeErr error) {
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
}

func (h *APIHandler) respondNoEnvChanges(c *gin.Context, ctx *envUpdateContext, merged map[string]string) {
	ms := int(time.Since(ctx.start).Milliseconds())
	ip := c.ClientIP()
	ua := c.GetHeader("User-Agent")

	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:      ctx.email,
		UserName:       ctx.name,
		ActionType:     "convox",
		Action:         audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionUpdate.String()),
		ResourceType:   "env",
		Resource:       ctx.app,
		Details:        `{"changes":"none"}`,
		IPAddress:      ip,
		UserAgent:      ua,
		Status:         "success",
		RBACDecision:   "allow",
		HTTPStatus:     http.StatusOK,
		ResponseTimeMs: ms,
	})

	c.JSON(
		http.StatusOK,
		UpdateEnvValuesResponse{Env: maskEnvForResponse(merged, ctx.extraSecrets, ctx.canViewSecrets)},
	)
}
