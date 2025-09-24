package handlers

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/envutil"
	"github.com/DocSpring/convox-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/convox-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// APIHandler handles regular API endpoints
type APIHandler struct {
	rbac            rbac.RBACManager
	database        *db.Database
	config          *config.Config
	rackCertManager *rackcert.Manager
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(rbac rbac.RBACManager, database *db.Database, config *config.Config, rackCertManager *rackcert.Manager) *APIHandler {
	return &APIHandler{
		rbac:            rbac,
		database:        database,
		config:          config,
		rackCertManager: rackCertManager,
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

// GetMe godoc
// @Summary Get current user profile
// @Description Returns the authenticated user's profile, roles, and default rack summary.
// @Tags Me
// @Produce json
// @Success 200 {object} CurrentUserResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /me [get]
func (h *APIHandler) GetMe(c *gin.Context) {
	email := c.GetString("user_email")
	name := c.GetString("user_name")
	rolesVal, _ := c.Get("user_roles")
	roles := normalizeStringSlice(rolesVal)

	user, err := h.rbac.GetUser(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user"})
		return
	}

	response := CurrentUserResponse{
		Email:       email,
		Name:        name,
		Roles:       roles,
		Permissions: user.Roles,
	}

	if rc, ok := h.primaryRack(); ok {
		alias := strings.TrimSpace(rc.Alias)
		if alias == "" {
			alias = strings.TrimSpace(rc.Name)
			if alias == "" {
				alias = "default"
			}
		}
		host := strings.TrimSpace(rc.URL)
		response.Rack = &RackSummary{
			Name:  rc.Name,
			Alias: alias,
			Host:  host,
		}
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
	defer resp.Body.Close()

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

	// Enforce env:view
	if ok, _ := h.rbac.Enforce(email, "env", "view"); !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to view environment variables."})
		return
	}

	allowedSecrets := false
	if wantSecrets {
		if ok, _ := h.rbac.Enforce(email, "secrets", "view"); !ok {
			res := app
			if key != "" {
				res = fmt.Sprintf("%s/%s", app, key)
			}
			details := map[string]interface{}{"app": app, "secrets": true}
			if key != "" {
				details["key"] = key
			}
			detailsJSON, _ := json.Marshal(details)
			_ = audit.LogDB(h.database, &db.AuditLog{
				UserEmail:      email,
				UserName:       name,
				ActionType:     "convox",
				Action:         "secrets.view",
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

	// Fetch latest env via rack API using configured rack
	rackConfig, exists := h.config.Racks["default"]
	if !exists {
		rackConfig, exists = h.config.Racks["local"]
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
			return
		}
	}

	if rackConfig.URL == "" || rackConfig.APIKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
		return
	}

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
	if !allowedSecrets {
		extra := strings.Split(os.Getenv("CONVOX_SECRET_ENV_VARS"), ",")
		if h.database != nil {
			if arr, err := h.database.GetProtectedEnvVars(); err == nil {
				extra = append(extra, arr...)
			}
		}
		for k := range envMap {
			if envutil.IsSecretKey(k, extra) {
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
	action := "env.view"
	resourceType := "env"
	if allowedSecrets {
		action = "secrets.view"
		resourceType = "secret"
	}
	_ = audit.LogDB(h.database, &db.AuditLog{
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
