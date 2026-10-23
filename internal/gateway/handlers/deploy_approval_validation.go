package handlers

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
)

// maxDeployApprovalMessageChars caps the message CI sends with a request. It's shown to the approver and kept in
// the audit log, where a long one would crowd out the request's other details.
const maxDeployApprovalMessageChars = 2000

// maxGitBranchChars is the longest branch name a request can name (the git_branch column's length).
const maxGitBranchChars = 255

// convoxAppName matches the app names a Convox rack accepts. Each app is a Kubernetes namespace (<rack>-<app>), so
// its name is a lowercase RFC 1123 label: letters, digits and dashes, starting and ending with a letter or digit.
var convoxAppName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// validateAppExists verifies that the specified app exists in the Convox rack. The app name is checked before it
// goes into the rack URL, and the rack is called with its pinned TLS certificate, as the proxy calls it.
// Returns false (after writing the response) if validation fails or the app doesn't exist.
func (h *APIHandler) validateAppExists(c *gin.Context, app string) bool {
	// Convox reserves "rack" and "system", so no app has those names.
	if !convoxAppName.MatchString(app) || app == "rack" || app == "system" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "app must be a Convox app name (lowercase letters, digits and dashes)",
		})
		return false
	}

	if rack, ok := h.primaryRack(); !ok || rack.URL == "" {
		gtwlog.Warnf("deploy approvals: cannot validate app %s - Convox rack not configured", app)
		return true // Fail open if Convox is not configured
	}
	rack, tlsCfg, ok := h.acquireRackContext(c)
	if !ok {
		return false
	}

	status, err := rackAppStatus(c.Request.Context(), rack, tlsCfg, app)
	if fpErr, mismatch := rackcert.AsFingerprintMismatch(err); mismatch {
		log.Printf(`{"level":"error","event":"rack_tls_verification_failed","scope":"deploy_approval_app",`+
			`"expected_fingerprint":"%s","actual_fingerprint":"%s"}`, fpErr.Expected, fpErr.Actual)
		c.JSON(http.StatusBadGateway, gin.H{"error": "rack certificate verification failed"})
		return false
	}
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to validate app %s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate app"})
		return false
	}

	if status == http.StatusNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("app '%s' not found", app)})
		return false
	}
	if status < 200 || status >= 300 {
		gtwlog.Warnf("deploy approvals: unexpected status validating app %s: %d", app, status)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate app"})
		return false
	}
	return true
}

// rackAppStatus asks the rack for app and returns the response status.
func rackAppStatus(ctx context.Context, rack config.RackConfig, tlsCfg *tls.Config, app string) (int, error) {
	appURL := strings.TrimRight(rack.URL, "/") + "/apps/" + url.PathEscape(app)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appURL, http.NoBody)
	if err != nil {
		return 0, err
	}
	username := rack.Username
	if username == "" {
		username = "convox"
	}
	req.SetBasicAuth(username, rack.APIKey)

	resp, err := httpclient.NewRackClient(10*time.Second, tlsCfg).Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// validateRequestFields validates and extracts required fields from the create request.
// Returns the validated message, git commit hash, and app name.
// If validation fails, it writes an appropriate error response and returns false.
// gitBranchProblem says why branch can't be a request's branch, or returns "". Git refuses control characters in
// branch names, and one in a request could forge lines in the gateway's logs.
func gitBranchProblem(branch string) string {
	if strings.IndexFunc(branch, unicode.IsControl) >= 0 {
		return "git_branch can't contain control characters"
	}
	if utf8.RuneCountInString(strings.TrimSpace(branch)) > maxGitBranchChars {
		return fmt.Sprintf("git_branch must be at most %d characters", maxGitBranchChars)
	}
	return ""
}

func (h *APIHandler) validateRequestFields(
	c *gin.Context,
	req CreateDeployApprovalRequestRequest,
) (string, string, string, bool) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return "", "", "", false
	}
	if utf8.RuneCountInString(message) > maxDeployApprovalMessageChars {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("message must be at most %d characters", maxDeployApprovalMessageChars),
		})
		return "", "", "", false
	}

	gitCommitHash, ok := db.NormalizeCommitSHA(req.GitCommitHash)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git_commit_hash must be a full 40-character commit SHA"})
		return "", "", "", false
	}

	if problem := gitBranchProblem(req.GitBranch); problem != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
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
