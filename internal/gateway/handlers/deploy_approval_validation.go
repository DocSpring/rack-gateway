package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// validateAppExists verifies that the specified app exists in the Convox rack.
// It calls the Convox API to check if the app is present.
// Returns false if validation fails or app doesn't exist, true otherwise.
func (h *APIHandler) validateAppExists(c *gin.Context, app string) bool {
	rack, ok := h.primaryRack()
	if !ok || rack.URL == "" {
		gtwlog.Warnf("deploy approvals: cannot validate app %s - Convox rack not configured", app)
		return true // Fail open if Convox is not configured
	}

	url := fmt.Sprintf("%s/apps/%s", rack.URL, app)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to create request to validate app %s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate app"})
		return false
	}

	username := rack.Username
	if username == "" {
		username = "convox"
	}
	if rack.APIKey != "" {
		req.SetBasicAuth(username, rack.APIKey)
	}

	client := httpclient.NewRackClient(10*time.Second, nil)
	resp, err := client.Do(req)
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to validate app %s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate app"})
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("app '%s' not found", app)})
		return false
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		gtwlog.Warnf("deploy approvals: unexpected status validating app %s: %d", app, resp.StatusCode)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate app"})
		return false
	}

	return true
}

// validateRequestFields validates and extracts required fields from the create request.
// Returns the validated message, git commit hash, and app name.
// If validation fails, it writes an appropriate error response and returns false.
func (h *APIHandler) validateRequestFields(
	c *gin.Context,
	req CreateDeployApprovalRequestRequest,
) (string, string, string, bool) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return "", "", "", false
	}

	gitCommitHash := strings.TrimSpace(req.GitCommitHash)
	if gitCommitHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git_commit_hash is required"})
		return "", "", "", false
	}

	app := strings.TrimSpace(req.App)
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app is required"})
		return "", "", "", false
	}

	if !h.validateAppExists(c, app) {
		return "", "", "", false
	}

	return message, gitCommitHash, app, true
}
