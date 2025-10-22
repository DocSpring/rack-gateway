package handlers

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/slack"
	"github.com/gin-gonic/gin"
)

// enforceIntegrationPermission checks if the user has the required permission for integrations.
// Returns true if allowed, false otherwise (and writes the error response to the context).
func (h *AdminHandler) enforceIntegrationPermission(c *gin.Context, action rbac.Action) bool {
	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return false
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceIntegration, action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return false
	}
	if !allowed {
		if action == rbac.ActionRead {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		} else {
			c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		}
		return false
	}

	return true
}

// loadSlackIntegration retrieves the Slack integration from the database.
// Returns the integration if found, nil otherwise (and writes the error response to the context).
func (h *AdminHandler) loadSlackIntegration(c *gin.Context) *db.SlackIntegration {
	integration, err := h.database.GetSlackIntegration()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get integration"})
		return nil
	}
	if integration == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no Slack integration found"})
		return nil
	}
	return integration
}

// createSlackClient decrypts the bot token and creates a Slack client.
// Returns the client if successful, nil otherwise (and writes the error response to the context).
func (h *AdminHandler) createSlackClient(c *gin.Context, integration *db.SlackIntegration) *slack.Client {
	botToken, err := base64.StdEncoding.DecodeString(integration.BotTokenEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return nil
	}
	return slack.NewClient(string(botToken))
}
