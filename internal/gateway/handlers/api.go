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
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
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

func (h *APIHandler) acquireRackContext(c *gin.Context) (config.RackConfig, *tls.Config, bool) {
	rackConfig, tlsCfg, err := h.rackContext(c.Request.Context())
	if err == nil {
		return rackConfig, tlsCfg, true
	}

	switch {
	case errors.Is(err, errRackNotConfigured):
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
	case errors.Is(err, errRackTLSConfig):
		log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack TLS"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack connection"})
	}

	return config.RackConfig{}, nil, false
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

	var dbUser *db.User
	authUser, _ := auth.GetAuthUser(c.Request.Context())
	if authUser != nil {
		dbUser = authUser.DBUser
	}
	if dbUser == nil {
		var err error
		dbUser, err = h.database.GetUser(email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user profile"})
			return
		}
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

	// Debug logging for step-up state
	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "auth_info_step_up_check user_email=%q has_auth_user=%t has_session=%t", email, authUser != nil, authUser != nil && authUser.Session != nil)
	if authUser != nil && authUser.Session != nil {
		var recentStepUpAt *time.Time
		if authUser.Session.RecentStepUpAt != nil {
			recentStepUpAt = authUser.Session.RecentStepUpAt
			expires := authUser.Session.RecentStepUpAt.Add(h.stepUpWindow())
			userInfo.RecentStepUpExpiresAt = &expires
			gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "auth_info_step_up_set user_email=%q recent_step_up_at=%q expires_at=%q", email, authUser.Session.RecentStepUpAt.Format(time.RFC3339), expires.Format(time.RFC3339))
		} else {
			gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "auth_info_step_up_nil user_email=%q session_id=%d", email, authUser.Session.ID)
		}
		if authUser.Session.TrustedDeviceID != nil && *authUser.Session.TrustedDeviceID > 0 {
			userInfo.HasTrustedDevice = true
		}
		_ = recentStepUpAt // Avoid unused variable warning
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
		CircleCI: h.config != nil && strings.TrimSpace(h.config.CircleCIToken) != "",
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
		// Log audit event even when no changes were made
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
