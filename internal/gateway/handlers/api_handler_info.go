package handlers

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/gin-gonic/gin"
)

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

	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "auth_info_step_up_check user_email=%q has_auth_user=%t has_session=%t", email, authUser != nil, authUser != nil && authUser.Session != nil)
	if authUser != nil && authUser.Session != nil {
		if authUser.Session.RecentStepUpAt != nil {
			expires := authUser.Session.RecentStepUpAt.Add(h.stepUpWindow())
			userInfo.RecentStepUpExpiresAt = &expires
			gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "auth_info_step_up_set user_email=%q recent_step_up_at=%q expires_at=%q", email, authUser.Session.RecentStepUpAt.Format(time.RFC3339), expires.Format(time.RFC3339))
		} else {
			gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "auth_info_step_up_nil user_email=%q session_id=%d", email, authUser.Session.ID)
		}
		if authUser.Session.TrustedDeviceID != nil && *authUser.Session.TrustedDeviceID > 0 {
			userInfo.HasTrustedDevice = true
		}
	}

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
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type"})
		return
	}

	idsParam := strings.TrimSpace(c.Query("ids"))
	if idsParam == "" {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

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
	rackConfig, exists := h.config.Racks["default"]
	if !exists {
		rackConfig, exists = h.config.Racks["local"]
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no rack configured"})
			return
		}
	}

	base := strings.TrimRight(rackConfig.URL, "/")
	url := base + "/system"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

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
	defer resp.Body.Close() //nolint:errcheck

	var rackInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rackInfo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode rack info"})
		return
	}

	c.JSON(http.StatusOK, rackInfo)
}
