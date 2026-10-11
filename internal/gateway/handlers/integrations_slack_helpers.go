package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/slack"
)

// enforceIntegrationPermission checks if the user has the required permission for integrations.
// Returns true if allowed, false otherwise (and writes the error response to the context).
func (h *AdminHandler) enforceIntegrationPermission(c *gin.Context, action rbac.Action) bool {
	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return false
	}

	if !callerCan(c, h.rbac, rbac.Gateway(rbac.ResourceIntegration, action)) {
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

// createSlackClient creates a Slack client with the integration's bot token.
// Returns nil when the stored token can't be decrypted (and writes the error response to the context).
func (_ *AdminHandler) createSlackClient(c *gin.Context, integration *db.SlackIntegration) *slack.Client {
	if integration.BotTokenErr != nil {
		CaptureErrorWithStatus(c, integration.BotTokenErr, http.StatusInternalServerError, "failed to decrypt token")
		return nil
	}
	return slack.NewClient(integration.BotToken)
}
