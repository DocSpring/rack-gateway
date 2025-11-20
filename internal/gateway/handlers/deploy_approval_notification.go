package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/github"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobgithub "github.com/DocSpring/rack-gateway/internal/gateway/jobs/github"
	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func (h *APIHandler) sendSlackNotification(c *gin.Context, recordID int64) {
	if h.jobsClient == nil || h.config == nil || h.config.Domain == "" {
		return
	}

	_, err := h.jobsClient.Insert(c.Request.Context(), jobslack.DeployApprovalArgs{
		DeployApprovalRequestID: recordID,
		GatewayDomain:           h.config.Domain,
	}, &river.InsertOpts{
		Queue:       jobs.QueueNotifications,
		MaxAttempts: jobs.MaxAttemptsNotification,
	})
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to enqueue Slack notification: %v", err)
		sentry.CaptureException(err)
	}
}

func (h *APIHandler) shouldPostPRComment(prURL string) bool {
	return h.settingsService != nil && prURL != "" && h.config != nil && h.config.GitHubToken != ""
}

func (h *APIHandler) postPRCommentAsync(c *gin.Context, app, prURL, publicID string) {
	postComment, err := getAppSettingBool(h.settingsService, app, settings.KeyGitHubPostPRComment, true)
	if err != nil {
		log.Printf("WARN: Failed to get github_post_pr_comment setting: %v", err)
		return
	}

	if !postComment {
		return
	}

	githubRepo, err := getAppSettingString(h.settingsService, app, settings.KeyVCSRepo, "")
	if err != nil {
		log.Printf("WARN: Failed to get vcs_repo setting: %v", err)
		return
	}

	if githubRepo == "" {
		return
	}

	approvalURL := h.buildApprovalURL(publicID)
	rackName := h.rackDisplay()

	prNumber, err := github.ExtractPRNumber(prURL)
	if err != nil {
		log.Printf("WARN: Failed to extract PR number from URL %s: %v", prURL, err)
		return
	}

	owner, repo := github.SplitRepo(githubRepo)
	if owner == "" || repo == "" {
		log.Printf("WARN: Invalid vcs_repo format: %s", githubRepo)
		return
	}

	comment := fmt.Sprintf(
		"#### Deploy Approval Request for %s\n\n"+
			"A deploy approval request has been created for this PR.\n\n**View request:** %s",
		rackName,
		approvalURL,
	)

	h.enqueueGitHubComment(c, owner, repo, prNumber, comment)
}

func (h *APIHandler) buildApprovalURL(publicID string) string {
	gatewayURL := h.config.Domain
	if gatewayURL == "" || gatewayURL == "localhost" {
		gatewayURL = fmt.Sprintf("http://localhost:%s", h.config.Port)
	} else {
		gatewayURL = fmt.Sprintf("https://%s", gatewayURL)
	}
	return fmt.Sprintf("%s/app/deploy-approval-requests/%s", gatewayURL, publicID)
}

func (h *APIHandler) enqueueGitHubComment(c *gin.Context, owner, repo string, prNumber int, comment string) {
	if h.jobsClient == nil {
		return
	}

	_, err := h.jobsClient.Insert(c.Request.Context(), jobgithub.PostPRCommentArgs{
		GitHubToken: h.config.GitHubToken,
		Owner:       owner,
		Repo:        repo,
		PRNumber:    prNumber,
		Comment:     comment,
	}, &river.InsertOpts{
		Queue:       jobs.QueueIntegrations,
		MaxAttempts: jobs.MaxAttemptsNotification,
	})
	if err != nil {
		log.Printf("ERROR: Failed to enqueue GitHub PR comment job: %v", err)
	}
}

func (h *APIHandler) authorizeViewRequest(
	c *gin.Context,
	userEmail string,
	dbUser *db.User,
	record *db.DeployApprovalRequest,
) bool {
	allowedAdmin, err := h.rbac.Enforce(
		userEmail,
		rbac.ScopeGateway,
		rbac.ResourceDeployApprovalRequest,
		rbac.ActionApprove,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return false
	}

	ownsRequest := record.CreatedByUserID != nil && *record.CreatedByUserID == dbUser.ID
	ownsToken := record.TargetUserID != nil && *record.TargetUserID == dbUser.ID

	if !allowedAdmin && !ownsRequest && !ownsToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return false
	}

	return true
}
