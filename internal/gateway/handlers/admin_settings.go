package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// GetConfig godoc
// @Summary Get legacy configuration
// @Description Returns the legacy user/domain configuration payload (deprecated).
// @Tags Config
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/config [get]
func (h *AdminHandler) GetConfig(c *gin.Context) {
	// Get users from the manager
	users, err := h.rbac.GetUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get users"})
		return
	}

	// Convert internal format to API format
	apiConfig := gin.H{
		"domain": h.rbac.GetAllowedDomain(),
		"users":  users,
	}

	c.JSON(http.StatusOK, apiConfig)
}

// UpdateConfig godoc
// @Summary Update legacy configuration
// @Description Placeholder endpoint retained for backwards compatibility. Always returns 501.
// @Tags Config
// @Accept json
// @Produce json
// @Failure 501 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/config [put]
func (h *AdminHandler) UpdateConfig(c *gin.Context) {
	// Would update configuration
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// GetSettings godoc
// @Summary Get gateway admin settings
// @Description Returns administrative settings including protected env vars and rack TLS state.
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security SessionCookie
// @Router /admin/settings [get]
func (h *AdminHandler) GetSettings(c *gin.Context) {
	resp := make(map[string]interface{})

	if h.database != nil {
		if arr, err := h.database.GetProtectedEnvVars(); err == nil {
			resp["protected_env_vars"] = arr
		} else {
			resp["protected_env_vars"] = []string{}
		}
		if v, err := h.database.GetAllowDestructiveActions(); err == nil {
			resp["allow_destructive_actions"] = v
		} else {
			resp["allow_destructive_actions"] = false
		}
		if arr, err := h.database.GetApprovedCommands(); err == nil {
			resp["approved_commands"] = arr
		} else {
			resp["approved_commands"] = []string{}
		}
		if patterns, err := h.database.GetAppImagePatterns(); err == nil {
			resp["app_image_patterns"] = patterns
		} else {
			resp["app_image_patterns"] = map[string]string{}
		}
		if settings, err := h.database.GetMFASettings(); err == nil && settings != nil {
			h.mfaSettings = settings
		}
	} else {
		resp["protected_env_vars"] = []string{}
		resp["allow_destructive_actions"] = false
		resp["approved_commands"] = []string{}
		resp["app_image_patterns"] = map[string]string{}
	}

	if h.mfaSettings != nil {
		resp["mfa"] = gin.H{
			"require_all_users":       h.mfaSettings.RequireAllUsers,
			"trusted_device_ttl_days": h.mfaSettings.TrustedDeviceTTLDays,
			"step_up_window_minutes":  h.mfaSettings.StepUpWindowMinutes,
		}
	}

	if h.config != nil {
		resp["sentry_tests_enabled"] = h.config.SentryTestsEnabled
	}

	pinningEnabled := h.config != nil && h.config.RackTLSPinningEnabled
	resp["rack_tls_pinning_enabled"] = pinningEnabled
	if pinningEnabled && h.rackCertMgr != nil {
		if cert, ok, err := h.rackCertMgr.CurrentCertificate(c.Request.Context()); err == nil && ok {
			resp["rack_tls_cert"] = gin.H{
				"pem":         cert.PEM,
				"fingerprint": cert.Fingerprint,
				"fetched_at":  cert.FetchedAt,
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateProtectedEnvVars godoc
// @Summary Update protected environment variables
// @Description Replaces the list of protected environment variable keys.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateProtectedEnvVarsRequest true "Protected env vars"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/protected_env_vars [put]
func (h *AdminHandler) UpdateProtectedEnvVars(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateProtectedEnvVarsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Normalize and de-dup to uppercase
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.ProtectedEnvVars))
	for _, k := range payload.ProtectedEnvVars {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}

	// Determine updating user id if available
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertSetting("protected_env_vars", out, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
			return
		}

		h.notifySettingsChanged(c, "protected_env_vars", strings.Join(out, ", "))
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateApprovedCommands godoc
// @Summary Update approved commands for CI/CD exec
// @Description Replaces the list of approved commands that CI/CD tokens can execute in processes.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateApprovedCommandsRequest true "Approved commands"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/approved_commands [put]
func (h *AdminHandler) UpdateApprovedCommands(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateApprovedCommandsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Trim and de-dup
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.ApprovedCommands))
	for _, cmd := range payload.ApprovedCommands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		if _, ok := seen[cmd]; ok {
			continue
		}
		seen[cmd] = struct{}{}
		out = append(out, cmd)
	}

	// Determine updating user id if available
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpdateApprovedCommands(out, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save approved commands"})
			return
		}

		h.notifySettingsChanged(c, "approved_commands", fmt.Sprintf("%d commands", len(out)))
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateAppImagePatterns godoc
// @Summary Update app image tag validation patterns
// @Description Updates the map of app names to image tag regex patterns used for manifest validation
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateAppImagePatternsRequest true "App image tag patterns"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/app_image_patterns [put]
func (h *AdminHandler) UpdateAppImagePatterns(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateAppImagePatternsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Validate and clean patterns
	patterns := make(map[string]string)
	for app, pattern := range payload.AppImagePatterns {
		app = strings.TrimSpace(app)
		pattern = strings.TrimSpace(pattern)
		if app == "" || pattern == "" {
			continue
		}
		patterns[app] = pattern
	}

	// Determine updating user id if available
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertAppImagePatterns(patterns, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save app image tag patterns"})
			return
		}

		// Format patterns for email notification
		var parts []string
		for app, pattern := range patterns {
			parts = append(parts, fmt.Sprintf("%s: %s", app, pattern))
		}
		sort.Strings(parts) // Ensure consistent ordering
		notifyValue := strings.Join(parts, ", ")
		if notifyValue == "" {
			notifyValue = "(none)"
		}

		h.notifySettingsChanged(c, "app_image_patterns", notifyValue)
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateAllowDestructiveActions godoc
// @Summary Toggle destructive action protections
// @Description Enables or disables destructive actions such as rack resets.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateAllowDestructiveActionsRequest true "Toggle payload"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/allow_destructive_actions [put]
func (h *AdminHandler) UpdateAllowDestructiveActions(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateAllowDestructiveActionsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertSetting("allow_destructive_actions", payload.AllowDestructiveActions, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
			return
		}

		h.notifySettingsChanged(c, "allow_destructive_actions", strconv.FormatBool(payload.AllowDestructiveActions))
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateMFASettings godoc
// @Summary Update MFA enforcement defaults
// @Description Configures whether MFA is required for all users.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateMFASettingsRequest true "MFA settings payload"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/mfa [put]
func (h *AdminHandler) UpdateMFASettings(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateMFASettingsRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if h.database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database unavailable"})
		return
	}

	settings, err := h.database.GetMFASettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load mfa settings"})
		return
	}
	settings.RequireAllUsers = payload.RequireAllUsers

	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if err := h.database.UpsertMFASettings(settings, uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save mfa settings"})
		return
	}

	if h.mfaSettings != nil {
		*h.mfaSettings = *settings
	} else {
		h.mfaSettings = settings
	}

	h.notifySettingsChanged(c, audit.BuildAction(rbac.ResourceStringMFA, audit.ActionVerbRequireAllUsers), strconv.FormatBool(settings.RequireAllUsers))

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// GetCircleCISettings godoc
// @Summary Get CircleCI integration settings
// @Description Returns CircleCI integration configuration including API token and approval job name.
// @Tags Settings
// @Produce json
// @Success 200 {object} db.CircleCISettings
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/settings/circleci [get]
func (h *AdminHandler) GetCircleCISettings(c *gin.Context) {
	if h.database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database unavailable"})
		return
	}

	enabled, err := h.database.CircleCIEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check circleci status"})
		return
	}
	if !enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "circleci integration not configured"})
		return
	}

	settings, err := h.database.GetCircleCISettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get circleci settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

func (h *AdminHandler) notifySettingsChanged(c *gin.Context, key, value string) {
	if h == nil || h.emailSender == nil {
		return
	}
	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	inviter := h.currentAuthUser(c)
	actorEmail := ""
	if inviter != nil {
		actorEmail = strings.TrimSpace(inviter.Email)
	}
	if actorEmail == "" {
		actorEmail = strings.TrimSpace(c.GetString("user_email"))
	}
	actorLabel := actorEmail
	if actorLabel == "" {
		actorLabel = "an administrator"
	}
	sort.Strings(admins)
	recipients := prioritiseInviterFirst(admins, actorEmail)
	rack := h.rackDisplay()
	subject := fmt.Sprintf("Rack Gateway (%s): %s changed the %s setting", rack, actorLabel, key)
	text, html, err := emailtemplates.RenderSettingsChanged(rack, actorLabel, key, value)
	if err != nil || (text == "" && html == "") {
		text = fmt.Sprintf("%s changed %s to %s.", actorLabel, key, value)
	}
	_ = h.emailSender.SendMany(recipients, subject, text, html)
}
