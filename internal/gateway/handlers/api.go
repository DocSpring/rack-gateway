package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/envutil"
	"github.com/DocSpring/convox-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// APIHandler handles regular API endpoints
type APIHandler struct {
	rbac     rbac.RBACManager
	database *db.Database
	config   *config.Config
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(rbac rbac.RBACManager, database *db.Database, config *config.Config) *APIHandler {
	return &APIHandler{
		rbac:     rbac,
		database: database,
		config:   config,
	}
}

// GetMe returns the current user's information
func (h *APIHandler) GetMe(c *gin.Context) {
	email := c.GetString("user_email")
	name := c.GetString("user_name")
	roles, _ := c.Get("user_roles")

	user, err := h.rbac.GetUser(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"email":       email,
		"name":        name,
		"roles":       roles,
		"permissions": user.Roles, // Would expand to actual permissions
	})
}

// GetCreatedBy returns creator information for resources
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

// GetRackInfo returns information about the current rack
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

	// Make the request to the actual rack using the shared client with insecure TLS
	client := httpclient.NewRackClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch rack info"})
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

// GetEnvValues returns environment variables for an app
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

	envMap, err := envutil.FetchLatestEnvMap(rackConfig, app)
	if err != nil {
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

	c.JSON(http.StatusOK, gin.H{"env": envMap})
}
