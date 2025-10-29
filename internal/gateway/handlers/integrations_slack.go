package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/slack"
	"github.com/gin-gonic/gin"
)

// Default channel action mappings for new integrations
var defaultChannelActions = map[string]interface{}{
	"security": map[string]interface{}{
		"id":   nil,
		"name": "#security",
		"actions": []string{
			audit.BuildAction(audit.ActionScopeLogin, audit.ActionVerbComplete),
			audit.ActionScopeLogin + ".*_failed", // Matches oauth_failed, user_not_authorized, etc.
			audit.ActionScopeMFAMethod + ".*",    // MFA enrollment events
			audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUpdateRoles),
			audit.ActionScopeAPIToken + ".*", // Matches all API token actions
		},
	},
	"infrastructure": map[string]interface{}{
		"id":   nil,
		"name": "#infrastructure",
		"actions": []string{
			rbac.ResourceDeployApprovalRequest.String() + ".*", // Matches all deploy approval actions
			audit.BuildAction(rbac.ResourceRelease.String(), rbac.ActionPromote.String()),
		},
	},
}

// Slack OAuth state management (in production, use Redis or database)
var oauthStates = make(map[string]bool)

// SlackOAuthAuthorizeHandler initiates the Slack OAuth flow
func (h *AdminHandler) SlackOAuthAuthorizeHandler(c *gin.Context) {
	if h.config.SlackClientID == "" || h.config.SlackClientSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Slack integration not configured"})
		return
	}

	if !h.enforceIntegrationPermission(c, rbac.ActionManage) {
		return
	}

	// Generate state token for CSRF protection
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
		return
	}
	state := base64.URLEncoding.EncodeToString(stateBytes)
	oauthStates[state] = true

	// Build redirect URI
	scheme := "https"
	if h.config.DevMode {
		scheme = "http"
	}
	host := c.Request.Host
	redirectURI := fmt.Sprintf("%s://%s/api/v1/integrations/slack/oauth/callback", scheme, host)

	// Build Slack authorization URL
	authURL := fmt.Sprintf(
		"https://slack.com/oauth/v2/authorize?client_id=%s&scope=%s&redirect_uri=%s&state=%s",
		url.QueryEscape(h.config.SlackClientID),
		url.QueryEscape("channels:read,chat:write"),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)

	c.JSON(http.StatusOK, gin.H{"authorization_url": authURL})
}

// SlackOAuthCallbackHandler handles the OAuth callback from Slack
func (h *AdminHandler) SlackOAuthCallbackHandler(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")

	if errorParam != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Slack OAuth error: %s", errorParam)})
		return
	}

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code or state"})
		return
	}

	// Verify state token
	if !oauthStates[state] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state token"})
		return
	}
	delete(oauthStates, state)

	// Build redirect URI (must match authorization request)
	scheme := "https"
	if h.config.DevMode {
		scheme = "http"
	}
	host := c.Request.Host
	redirectURI := fmt.Sprintf("%s://%s/api/v1/integrations/slack/oauth/callback", scheme, host)

	// Exchange code for access token
	oauthResp, err := slack.ExchangeOAuthCode(h.config.SlackClientID, h.config.SlackClientSecret, code, redirectURI)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("OAuth exchange failed: %v", err)})
		return
	}

	// Get current user for created_by tracking
	userEmail := strings.TrimSpace(c.GetString("user_email"))
	var createdByUserID *int64
	if userEmail != "" {
		if user, err := h.database.GetUser(userEmail); err == nil && user != nil {
			createdByUserID = &user.ID
		}
	}

	// Encrypt bot token (for now, just base64 encode - in production use proper encryption)
	botTokenEncrypted := base64.StdEncoding.EncodeToString([]byte(oauthResp.AccessToken))

	// Delete existing integration if any
	_ = h.database.DeleteSlackIntegration()

	// Create new integration with default channel mappings
	_, err = h.database.CreateSlackIntegration(
		oauthResp.Team.ID,
		oauthResp.Team.Name,
		botTokenEncrypted,
		oauthResp.BotUserID,
		oauthResp.Scope,
		defaultChannelActions,
		createdByUserID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save integration"})
		return
	}

	// Redirect to integrations page
	c.Redirect(http.StatusFound, "/app/integrations?slack=connected")
}

// GetSlackIntegrationHandler retrieves the current Slack integration
// @Summary Get Slack integration
// @Tags integrations
// @Produce json
// @Success 200 {object} db.SlackIntegration
// @Failure 404 {object} ErrorResponse
// @Router /integrations/slack [get]
func (h *AdminHandler) GetSlackIntegrationHandler(c *gin.Context) {
	if !h.enforceIntegrationPermission(c, rbac.ActionRead) {
		return
	}

	integration := h.loadSlackIntegration(c)
	if integration == nil {
		return
	}

	// Don't expose the encrypted token
	integration.BotTokenEncrypted = ""

	c.JSON(http.StatusOK, integration)
}

// UpdateSlackChannelsHandler updates the channel action mappings
func (h *AdminHandler) UpdateSlackChannelsHandler(c *gin.Context) {
	if !h.enforceIntegrationPermission(c, rbac.ActionManage) {
		return
	}

	var req struct {
		ChannelActions map[string]interface{} `json:"channel_actions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := h.database.UpdateSlackIntegrationChannels(req.ChannelActions); err != nil {
		if err.Error() == "no Slack integration found to update" {
			c.JSON(http.StatusNotFound, gin.H{"error": "no Slack integration configured"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update channels"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteSlackIntegrationHandler removes the Slack integration
func (h *AdminHandler) DeleteSlackIntegrationHandler(c *gin.Context) {
	if !h.enforceIntegrationPermission(c, rbac.ActionManage) {
		return
	}

	if err := h.database.DeleteSlackIntegration(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete integration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListSlackChannelsHandler lists available Slack channels
func (h *AdminHandler) ListSlackChannelsHandler(c *gin.Context) {
	if !h.enforceIntegrationPermission(c, rbac.ActionRead) {
		return
	}

	integration := h.loadSlackIntegration(c)
	if integration == nil {
		return
	}

	client := h.createSlackClient(c, integration)
	if client == nil {
		return
	}

	channels, err := client.ListChannels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list channels: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"channels": channels})
}

// TestSlackNotificationHandler sends a test notification
func (h *AdminHandler) TestSlackNotificationHandler(c *gin.Context) {
	if !h.enforceIntegrationPermission(c, rbac.ActionManage) {
		return
	}

	var req struct {
		ChannelID string `json:"channel_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		gtwlog.Warnf("slack test notification: invalid JSON request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.ChannelID == "" {
		gtwlog.Warnf("slack test notification: empty channel_id in request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id is required"})
		return
	}

	integration := h.loadSlackIntegration(c)
	if integration == nil {
		gtwlog.Warnf("slack test notification: no Slack integration configured")
		return
	}

	client := h.createSlackClient(c, integration)
	if client == nil {
		gtwlog.Errorf("slack test notification: failed to create Slack client")
		return
	}

	gtwlog.Infof("slack test notification: sending test message to channel %s", req.ChannelID)
	err := client.PostMessage(req.ChannelID, "🧪 Test notification from Rack Gateway", nil)
	if err != nil {
		gtwlog.Errorf("slack test notification: failed to send message: %v", err)

		// Provide user-friendly error messages for common Slack API errors
		errMsg := err.Error()
		if strings.Contains(errMsg, "not_in_channel") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Bot is not in the channel. Invite the bot with: /invite @Rack Gateway"})
			return
		}
		if strings.Contains(errMsg, "channel_not_found") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Channel not found"})
			return
		}
		if strings.Contains(errMsg, "is_archived") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Channel is archived"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to send message: %v", err)})
		return
	}

	gtwlog.Infof("slack test notification: successfully sent test message to channel %s", req.ChannelID)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UpdateSlackAlertSettingsHandler updates deploy approval alert configuration
func (h *AdminHandler) UpdateSlackAlertSettingsHandler(c *gin.Context) {
	if !h.enforceIntegrationPermission(c, rbac.ActionManage) {
		return
	}

	var req struct {
		DeployApprovalsEnabled   bool   `json:"deploy_approvals_enabled"`
		DeployApprovalsChannelID string `json:"deploy_approvals_channel_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Validate channel ID format if provided
	if req.DeployApprovalsChannelID != "" && !strings.HasPrefix(req.DeployApprovalsChannelID, "C") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel ID format"})
		return
	}

	if err := h.database.UpdateSlackAlertSettings(req.DeployApprovalsEnabled, req.DeployApprovalsChannelID); err != nil {
		if err.Error() == "no Slack integration found to update" {
			c.JSON(http.StatusNotFound, gin.H{"error": "no Slack integration configured"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update alert settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
