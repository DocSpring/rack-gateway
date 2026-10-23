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
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// logEnvUpdateDiffs writes one audit entry per env change. Its values are redacted unless the app's
// audit_visible_env_vars lists the key and it is neither secret nor protected.
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
	visible := h.auditVisibleKeys(app)

	for _, diff := range diffs {
		action, resourceType := audit.EnvChangeAction(diff)

		_ = h.auditLogger.LogRequestEntry(c.Request.Context(), &db.AuditLog{
			UserEmail:      email,
			UserName:       name,
			ActionType:     audit.ActionTypeConvox,
			Action:         action,
			ResourceType:   resourceType,
			Resource:       fmt.Sprintf("%s/%s", app, diff.Key),
			Details:        visible.AuditDetails(diff),
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
	wantSecrets := middleware.RevealsSecrets(c)
	start := time.Now()

	email := c.GetString("user_email")
	name := c.GetString("user_name")

	if !callerCan(c, h.rbac, rbac.Convox(rbac.ResourceEnv, rbac.ActionRead)) {
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

	extraSecrets, _, err := h.secretAndProtectedKeys(app)
	if err != nil {
		respondEnvSettingsError(c, app, err)
		return
	}
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

	if callerCan(c, h.rbac, rbac.Convox(rbac.ResourceSecret, rbac.ActionRead)) {
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
	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), &db.AuditLog{
		UserEmail:      email,
		UserName:       name,
		ActionType:     audit.ActionTypeConvox,
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

func (_ *APIHandler) maskSecretsIfNeeded(envMap map[string]string, secretKeys []string, allowed bool) {
	if allowed {
		return
	}
	for k, v := range envMap {
		if envutil.MaskedForReaders(k, v, secretKeys) {
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
	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), &db.AuditLog{
		UserEmail:      email,
		UserName:       name,
		ActionType:     audit.ActionTypeConvox,
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
// @Description Secrets and values with credentials in them remain masked unless the user has secrets permissions.
// @Description Answers 409 while another env change to the app is in progress, or while a build whose manifest
// @Description the gateway checked with the app's env may still read it.
// @Tags Environment
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param request body UpdateEnvValuesRequest true "Environment update payload"
// @Success 200 {object} UpdateEnvValuesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
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
	defer ctx.envLock.Release()

	merged, diffs, err := h.mergeEnvChanges(ctx)
	if err != nil {
		h.respondMergeError(c, err)
		return
	}

	if len(diffs) == 0 {
		h.respondNoEnvChanges(c, ctx, merged)
		return
	}
	if !h.checkNoBuildReadingEnv(c, ctx) {
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
	envLock        *db.AppEnvLock
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

	if !callerCan(c, h.rbac, rbac.Convox(rbac.ResourceEnv, rbac.ActionSet)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to modify environment variables."})
		return nil, false
	}

	rackConfig, tlsCfg, ok := h.acquireRackContext(c)
	if !ok {
		return nil, false
	}
	ctx.rackConfig = rackConfig
	ctx.tlsCfg = tlsCfg

	extraSecrets, protectedKeys, err := h.secretAndProtectedKeys(ctx.app)
	if err != nil {
		respondEnvSettingsError(c, ctx.app, err)
		return nil, false
	}
	ctx.extraSecrets, ctx.protectedKeys = extraSecrets, protectedKeys
	ctx.allowSecrets = callerCan(c, h.rbac, rbac.Convox(rbac.ResourceSecret, rbac.ActionSet))
	ctx.canViewSecrets = callerCan(c, h.rbac, rbac.Convox(rbac.ResourceSecret, rbac.ActionRead))

	// The lock is held from reading the env until the release is created (UpdateEnvValues releases it).
	if ctx.envLock, ok = h.lockAppEnv(c, ctx.app); !ok {
		return nil, false
	}
	baseEnv, ok := h.fetchEnvMap(c, "env_update", ctx.app, rackConfig, tlsCfg)
	if !ok {
		ctx.envLock.Release()
		return nil, false
	}
	ctx.baseEnv = baseEnv

	return ctx, true
}

// checkNoBuildReadingEnv answers 409 while a build the gateway checked may still read the app's env
// (envutil.CheckNoBuildReadingEnv), and reports whether the change may go ahead.
func (h *APIHandler) checkNoBuildReadingEnv(c *gin.Context, ctx *envUpdateContext) bool {
	err := envutil.CheckNoBuildReadingEnv(c.Request.Context(), h.database, ctx.rackConfig, ctx.tlsCfg, ctx.app)
	switch {
	case err == nil:
		return true
	case errors.Is(err, envutil.ErrBuildReadingEnv):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		log.Printf(`{"level":"error","event":"env_update_build_check_failed","app":%q,"error":%q}`,
			ctx.app, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to check the app's builds"})
	}
	return false
}

// lockAppEnv takes the app's env write lock (db.LockAppEnv), or answers 409 when another change holds it.
func (h *APIHandler) lockAppEnv(c *gin.Context, app string) (*db.AppEnvLock, bool) {
	if h.database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database unavailable"})
		return nil, false
	}
	lock, err := h.database.LockAppEnv(c.Request.Context(), app)
	switch {
	case errors.Is(err, db.ErrAppEnvLockBusy):
		c.JSON(http.StatusConflict, gin.H{"error": "Another env change to this app is in progress. Try again."})
		return nil, false
	case err != nil:
		log.Printf(`{"level":"error","event":"env_lock_failed","app":%q,"error":%q}`, app, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to lock the app's env"})
		return nil, false
	}
	return lock, true
}

func (_ *APIHandler) mergeEnvChanges(ctx *envUpdateContext) (map[string]string, []envutil.EnvDiff, error) {
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

func (_ *APIHandler) respondMergeError(c *gin.Context, mergeErr error) {
	switch {
	case errors.Is(mergeErr, envutil.ErrSecretPermission):
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to modify secrets."})
	case errors.Is(mergeErr, envutil.ErrProtectedEnvModification):
		c.JSON(http.StatusForbidden, gin.H{"error": "This environment variable is protected and cannot be changed."})
	case errors.Is(mergeErr, envutil.ErrInvalidEnvEntry):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Env var names must match [A-Za-z_][A-Za-z0-9_]* and values can't contain control characters.",
		})
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

	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), &db.AuditLog{
		UserEmail:      ctx.email,
		UserName:       ctx.name,
		ActionType:     audit.ActionTypeConvox,
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
