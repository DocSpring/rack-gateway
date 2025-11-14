package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/github"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// performGitHubVerification checks GitHub verification settings and performs verification if enabled.
// Returns the PR URL if found, or empty string if verification is disabled or no PR is required.
// Returns false if verification fails or settings cannot be loaded.
func (h *APIHandler) performGitHubVerification(
	c *gin.Context,
	app string,
	req CreateDeployApprovalRequestRequest,
	gitCommitHash string,
) (string, bool) {
	if h.settingsService == nil {
		return "", true
	}

	enabled, err := getAppSettingBool(h.settingsService, app, settings.KeyGitHubVerification, true)
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get github_verification setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return "", false
	}

	if !enabled || h.config == nil || h.config.GitHubToken == "" {
		return "", true
	}

	githubRepo, err := getAppSettingString(h.settingsService, app, settings.KeyVCSRepo, "")
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get vcs_repo setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return "", false
	}

	if githubRepo == "" {
		return "", true
	}

	return h.verifyGitHubCommit(c, app, githubRepo, req, gitCommitHash)
}

// verifyGitHubCommit performs the actual GitHub commit verification.
// It validates the commit exists on the specified branch and finds the associated PR if required.
// Returns the PR URL if found, or empty string if no PR is required.
// Returns false if verification fails.
func (h *APIHandler) verifyGitHubCommit(
	c *gin.Context,
	app string,
	githubRepo string,
	req CreateDeployApprovalRequestRequest,
	gitCommitHash string,
) (string, bool) {
	gitBranch := strings.TrimSpace(req.GitBranch)
	if gitBranch == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git_branch is required for GitHub verification"})
		return "", false
	}

	owner, repo := github.SplitRepo(githubRepo)
	if owner == "" || repo == "" {
		gtwlog.Warnf("deploy approvals: invalid vcs_repo format app=%s value=%s", app, githubRepo)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "GitHub integration misconfigured"})
		return "", false
	}

	if ok := h.checkDefaultBranchRestriction(c, app, gitBranch); !ok {
		return "", false
	}

	requirePR, verifyMode, ok := h.getVerificationSettings(c, app)
	if !ok {
		return "", false
	}

	client := github.NewClient(h.config.GitHubToken)
	opts := github.VerifyCommitOptions{
		RequirePR: requirePR,
		Mode:      verifyMode,
	}

	prURL, err := client.VerifyCommitAndFindPR(owner, repo, gitBranch, gitCommitHash, opts)
	if err != nil {
		gtwlog.Warnf(
			"deploy approvals: GitHub verification failed app=%s repo=%s/%s branch=%s commit=%s: %v",
			app,
			owner,
			repo,
			gitBranch,
			gitCommitHash,
			err,
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("GitHub verification failed: %s", err.Error())})
		return "", false
	}

	return prURL, true
}

// checkDefaultBranchRestriction checks if deploying from the default branch is allowed.
// Returns false if deployment from default branch is disabled and the branch matches the default.
func (h *APIHandler) checkDefaultBranchRestriction(c *gin.Context, app, gitBranch string) bool {
	allowDefault, err := getAppSettingBool(
		h.settingsService,
		app,
		settings.KeyAllowDeployFromDefaultBranch,
		false,
	)
	if err != nil {
		gtwlog.Warnf(
			"deploy approvals: failed to get allow_deploy_from_default_branch setting app=%s: %v",
			app,
			err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return false
	}

	defaultBranch, err := getAppSettingString(h.settingsService, app, settings.KeyDefaultBranch, "main")
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get default_branch setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return false
	}

	if !allowDefault && gitBranch == defaultBranch {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("deploying from default branch '%s' is not allowed", defaultBranch),
		})
		return false
	}

	return true
}

// getVerificationSettings retrieves GitHub verification settings for the app.
// Returns requirePR flag, verify mode, and success status.
func (h *APIHandler) getVerificationSettings(c *gin.Context, app string) (bool, string, bool) {
	requirePR, err := getAppSettingBool(h.settingsService, app, settings.KeyRequirePRForBranch, true)
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get require_pr_for_branch setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return false, "", false
	}

	verifyMode, err := getAppSettingString(
		h.settingsService,
		app,
		settings.KeyVerifyGitCommitMode,
		settings.VerifyGitCommitModeLatest,
	)
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get verify_git_commit_mode setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return false, "", false
	}

	return requirePR, verifyMode, true
}
