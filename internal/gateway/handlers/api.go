package handlers

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

// APIHandler handles regular API endpoints
type APIHandler struct {
	rbac            rbac.RBACManager
	database        *db.Database
	config          *config.Config
	rackCertManager *rackcert.Manager
	mfaSettings     *db.MFASettings
	auditLogger     *audit.Logger
	settingsService *settings.Service
}

var (
	errRackNotConfigured = errors.New("rack not configured")
	errRackTLSConfig     = errors.New("rack tls configuration failed")
)

// NewAPIHandler creates a new API handler
func NewAPIHandler(rbac rbac.RBACManager, database *db.Database, config *config.Config, rackCertManager *rackcert.Manager, mfaSettings *db.MFASettings, auditLogger *audit.Logger, settingsService *settings.Service) *APIHandler {
	return &APIHandler{
		rbac:            rbac,
		database:        database,
		config:          config,
		rackCertManager: rackCertManager,
		mfaSettings:     mfaSettings,
		auditLogger:     auditLogger,
		settingsService: settingsService,
	}
}

func (h *APIHandler) primaryRack() (config.RackConfig, bool) {
	if h == nil || h.config == nil {
		return config.RackConfig{}, false
	}
	if rc, ok := h.config.Racks["default"]; ok && rc.Enabled {
		return rc, true
	}
	if rc, ok := h.config.Racks["local"]; ok && rc.Enabled {
		return rc, true
	}
	return config.RackConfig{}, false
}

func (h *APIHandler) stepUpWindow() time.Duration {
	if h.mfaSettings != nil && h.mfaSettings.StepUpWindowMinutes > 0 {
		return time.Duration(h.mfaSettings.StepUpWindowMinutes) * time.Minute
	}
	return 10 * time.Minute
}

func (h *APIHandler) rackContext(ctx context.Context) (config.RackConfig, *tls.Config, error) {
	rc, ok := h.primaryRack()
	if !ok || strings.TrimSpace(rc.URL) == "" || strings.TrimSpace(rc.APIKey) == "" {
		return config.RackConfig{}, nil, errRackNotConfigured
	}

	var tlsCfg *tls.Config
	if h.rackCertManager != nil {
		cfg, err := h.rackCertManager.TLSConfig(ctx)
		if err != nil {
			return config.RackConfig{}, nil, fmt.Errorf("%w: %v", errRackTLSConfig, err)
		}
		tlsCfg = cfg
	}

	return rc, tlsCfg, nil
}

func (h *APIHandler) secretAndProtectedKeys(app string) ([]string, map[string]struct{}) {
	extra := make([]string, 0)
	seen := make(map[string]struct{})

	// Get secret env vars for this app
	if h.settingsService != nil {
		if arr, err := h.settingsService.GetSecretEnvVars(app); err == nil {
			for _, key := range arr {
				trim := strings.TrimSpace(key)
				if trim == "" {
					continue
				}
				upper := strings.ToUpper(trim)
				if _, ok := seen[upper]; !ok {
					extra = append(extra, trim)
					seen[upper] = struct{}{}
				}
			}
		}
	}

	// Fall back to environment variable for backward compatibility
	if raw := os.Getenv("CONVOX_SECRET_ENV_VARS"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			trim := strings.TrimSpace(part)
			if trim == "" {
				continue
			}
			upper := strings.ToUpper(trim)
			if _, ok := seen[upper]; !ok {
				extra = append(extra, trim)
				seen[upper] = struct{}{}
			}
		}
	}

	protected := make(map[string]struct{})
	if h.settingsService != nil {
		if arr, err := h.settingsService.GetProtectedEnvVars(app); err == nil {
			for _, key := range arr {
				trim := strings.TrimSpace(key)
				if trim == "" {
					continue
				}
				upper := strings.ToUpper(trim)
				protected[upper] = struct{}{}
				if _, ok := seen[upper]; !ok {
					extra = append(extra, trim)
					seen[upper] = struct{}{}
				}
			}
		}
	}

	return extra, protected
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
		action := audit.BuildAction(rbac.ResourceStringEnv, rbac.ActionStringSet)
		resourceType := "env"
		if diff.Secret {
			action = audit.BuildAction(rbac.ResourceStringSecret, rbac.ActionStringSet)
			resourceType = "secret"
			oldVal = "[REDACTED]"
			newVal = "[REDACTED]"
		}
		if strings.TrimSpace(diff.NewVal) == "" {
			if diff.Secret {
				action = audit.BuildAction(rbac.ResourceStringSecret, audit.ActionVerbUnset)
			} else {
				action = audit.BuildAction(rbac.ResourceStringEnv, audit.ActionVerbUnset)
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

// GetInfo godoc
// @Summary Get gateway information
// @Description Returns user, rack, and integrations status in a single request for app bootstrap
// @Tags Info
// @Produce json
// @Success 200 {object} InfoResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /info [get]
func (h *APIHandler) GetInfo(c *gin.Context) {
	email := c.GetString("user_email")
	name := c.GetString("user_name")
	rolesVal, _ := c.Get("user_roles")
	roles := normalizeStringSlice(rolesVal)

	dbUser, err := h.database.GetUser(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user profile"})
		return
	}

	// Build user info
	userInfo := UserInfo{
		Email:            email,
		Name:             name,
		Roles:            roles,
		MFAEnrolled:      false,
		MFARequired:      false,
		HasTrustedDevice: false,
	}

	if dbUser != nil {
		if strings.TrimSpace(userInfo.Name) == "" {
			userInfo.Name = dbUser.Name
		}
		userInfo.MFAEnrolled = dbUser.MFAEnrolled
		userInfo.PreferredMFAMethod = dbUser.PreferredMFAMethod
	}

	if shouldEnforceMFA(h.mfaSettings, dbUser) {
		userInfo.MFARequired = true
	}

	if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil && authUser.Session != nil {
		if authUser.Session.RecentStepUpAt != nil {
			expires := authUser.Session.RecentStepUpAt.Add(h.stepUpWindow())
			userInfo.RecentStepUpExpiresAt = &expires
		}
		if authUser.Session.TrustedDeviceID != nil && *authUser.Session.TrustedDeviceID > 0 {
			userInfo.HasTrustedDevice = true
		}
	}

	// Build rack info
	rc, ok := h.primaryRack()
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
		return
	}

	alias := strings.TrimSpace(rc.Alias)
	if alias == "" {
		alias = strings.TrimSpace(rc.Name)
		if alias == "" {
			alias = "default"
		}
	}

	rackInfo := RackSummary{
		Name:  rc.Name,
		Alias: alias,
		Host:  strings.TrimSpace(rc.URL),
	}

	// Build integrations info
	integrationsInfo := IntegrationsInfo{
		Slack:    h.config != nil && strings.TrimSpace(h.config.SlackClientID) != "" && strings.TrimSpace(h.config.SlackClientSecret) != "",
		GitHub:   h.config != nil && strings.TrimSpace(h.config.GitHubToken) != "",
		CircleCI: h.config != nil && strings.TrimSpace(h.config.CircleCIToken) != "" && strings.TrimSpace(h.config.CircleCIOrgSlug) != "",
	}

	response := InfoResponse{
		User:         userInfo,
		Rack:         rackInfo,
		Integrations: integrationsInfo,
	}

	c.JSON(http.StatusOK, response)
}

// GetCreatedBy godoc
// @Summary Get resource creator metadata
// @Description Returns creator information for the supplied resource identifiers.
// @Tags Metadata
// @Produce json
// @Param type query string true "Resource type" Enums(app,build,release)
// @Param ids query string false "Comma-separated resource IDs"
// @Success 200 {object} map[string]db.CreatorInfo
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /created-by [get]
func (h *APIHandler) GetCreatedBy(c *gin.Context) {
	typ := strings.TrimSpace(c.Query("type"))
	if typ == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type required"})
		return
	}

	switch typ {
	case "app", "build", "release":
		// Valid types
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type"})
		return
	}

	idsParam := strings.TrimSpace(c.Query("ids"))
	if idsParam == "" {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	// Parse and deduplicate IDs
	parts := strings.Split(idsParam, ",")
	ids := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		ids = append(ids, p)
	}

	if len(ids) == 0 {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	// Fetch from database
	creators, err := h.database.GetResourceCreators(typ, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch creators"})
		return
	}

	c.JSON(http.StatusOK, creators)
}

func normalizeStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// GetRackInfo godoc
// @Summary Get rack system information
// @Description Proxies the rack /system endpoint returning the rack's metadata.
// @Tags Rack
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 502 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /rack [get]
func (h *APIHandler) GetRackInfo(c *gin.Context) {
	// Get the default rack config
	rackConfig, exists := h.config.Racks["default"]
	if !exists {
		rackConfig, exists = h.config.Racks["local"]
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no rack configured"})
			return
		}
	}

	// Build upstream URL to fetch actual rack system info
	base := strings.TrimRight(rackConfig.URL, "/")
	url := base + "/system"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	// Basic auth to rack
	user := rackConfig.Username
	if user == "" {
		user = "convox"
	}
	authz := base64.StdEncoding.EncodeToString([]byte(user + ":" + rackConfig.APIKey))
	req.Header.Set("Authorization", "Basic "+authz)

	var tlsCfg *tls.Config
	if h.rackCertManager != nil {
		cfg, err := h.rackCertManager.TLSConfig(c.Request.Context())
		if err != nil {
			log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack TLS"})
			return
		}
		tlsCfg = cfg
	}

	client := httpclient.NewRackClient(10*time.Second, tlsCfg)
	resp, err := client.Do(req)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			log.Printf(`{"level":"error","event":"rack_tls_verification_failed","scope":"rack_info","expected_fingerprint":"%s","actual_fingerprint":"%s"}`, fpErr.Expected, fpErr.Actual)
			c.JSON(http.StatusBadGateway, gin.H{"error": "rack certificate verification failed"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch rack info"})
		return
	}
	defer resp.Body.Close() //nolint:errcheck // response cleanup

	// Parse response
	var rackInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rackInfo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode rack info"})
		return
	}

	c.JSON(http.StatusOK, rackInfo)
}

// GetEnvValues godoc
// @Summary Get environment variables
// @Description Returns environment variables for a Convox app, masking secrets unless authorized.
// @Tags Environment
// @Produce json
// @Param app query string true "App name"
// @Param key query string false "Specific key to fetch"
// @Param secrets query bool false "Include secret values"
// @Success 200 {object} EnvValuesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /env [get]
func (h *APIHandler) GetEnvValues(c *gin.Context) {
	app := strings.TrimSpace(c.Query("app"))
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing app"})
		return
	}

	key := c.Query("key")
	wantSecrets := strings.EqualFold(c.Query("secrets"), "true")
	start := time.Now()

	// Check permissions
	email := c.GetString("user_email")
	name := c.GetString("user_name")

	// Enforce env:read
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
				Action:         audit.BuildAction(rbac.ResourceStringSecret, rbac.ActionStringRead),
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

	rackConfig, tlsCfg, err := h.rackContext(c.Request.Context())
	if err != nil {
		if errors.Is(err, errRackNotConfigured) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
			return
		}
		if errors.Is(err, errRackTLSConfig) {
			log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack TLS"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack connection"})
		return
	}

	envMap, err := envutil.FetchLatestEnvMap(rackConfig, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			log.Printf(`{"level":"error","event":"rack_tls_verification_failed","scope":"env_fetch","expected_fingerprint":"%s","actual_fingerprint":"%s","app":"%s"}`, fpErr.Expected, fpErr.Actual, app)
			c.JSON(http.StatusBadGateway, gin.H{"error": "rack certificate verification failed"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch env"})
		return
	}

	// Mask secrets unless explicit access was granted
	extraSecrets, _ := h.secretAndProtectedKeys(app)
	if !allowedSecrets {
		for k := range envMap {
			if envutil.IsSecretKey(k, extraSecrets) {
				envMap[k] = envutil.MaskedSecret
			}
		}
	}

	// Filter by key if provided
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
	action := audit.BuildAction(rbac.ResourceStringEnv, rbac.ActionStringRead)
	resourceType := "env"
	if allowedSecrets {
		action = audit.BuildAction(rbac.ResourceStringSecret, rbac.ActionStringRead)
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
// @Param request body UpdateEnvValuesRequest true "Environment update payload"
// @Success 200 {object} UpdateEnvValuesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /env [put]
func (h *APIHandler) UpdateEnvValues(c *gin.Context) {
	start := time.Now()

	var req UpdateEnvValuesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	app := strings.TrimSpace(req.App)
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app is required"})
		return
	}

	email := c.GetString("user_email")
	name := c.GetString("user_name")

	if ok, _ := h.rbac.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionSet); !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to modify environment variables."})
		return
	}

	rackConfig, tlsCfg, err := h.rackContext(c.Request.Context())
	if err != nil {
		if errors.Is(err, errRackNotConfigured) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
			return
		}
		if errors.Is(err, errRackTLSConfig) {
			log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack TLS"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack connection"})
		return
	}

	baseEnv, err := envutil.FetchLatestEnvMap(rackConfig, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			log.Printf(`{"level":"error","event":"rack_tls_verification_failed","scope":"env_update","expected_fingerprint":"%s","actual_fingerprint":"%s","app":"%s"}`, fpErr.Expected, fpErr.Actual, app)
			c.JSON(http.StatusBadGateway, gin.H{"error": "rack certificate verification failed"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch env"})
		return
	}
	if baseEnv == nil {
		baseEnv = map[string]string{}
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
		// Log audit event even when no changes were made
		ms := int(time.Since(start).Milliseconds())
		ip := c.ClientIP()
		ua := c.GetHeader("User-Agent")
		_ = h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:      email,
			UserName:       name,
			ActionType:     "convox",
			Action:         audit.BuildAction(rbac.ResourceStringEnv, rbac.ActionStringUpdate),
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

		responseEnv := make(map[string]string, len(merged))
		for k, v := range merged {
			value := v
			if !canViewSecrets && envutil.IsSecretKey(k, extraSecrets) {
				value = envutil.MaskedSecret
			}
			responseEnv[k] = value
		}
		c.JSON(http.StatusOK, UpdateEnvValuesResponse{Env: responseEnv})
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

	responseEnv := make(map[string]string, len(merged))
	for k, v := range merged {
		value := v
		if !canViewSecrets && envutil.IsSecretKey(k, extraSecrets) {
			value = envutil.MaskedSecret
		}
		responseEnv[k] = value
	}

	c.JSON(http.StatusOK, UpdateEnvValuesResponse{Env: responseEnv, ReleaseID: releaseID})
}
