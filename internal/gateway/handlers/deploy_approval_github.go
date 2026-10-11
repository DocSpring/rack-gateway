package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/github"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// GitHub verification results recorded in the deploy_approval_request.create audit entry.
const (
	githubVerificationPassed  = "passed"
	githubVerificationSkipped = "skipped"
)

// githubVerification is what GitHub verification did for a new deploy approval request: the create audit entry
// records it as change-management evidence.
type githubVerification struct {
	result     string // githubVerificationPassed or githubVerificationSkipped
	skipReason string
	repo       string // owner/repo
	mode       string // settings.VerifyGitCommitModeLatest or settings.VerifyGitCommitModeBranch
	requirePR  bool
	prURL      string
}

func skippedGitHubVerification(reason string) githubVerification {
	return githubVerification{result: githubVerificationSkipped, skipReason: reason}
}

// auditFields returns the verification's fields for the create audit entry. A passed verification records the
// settings it checked against, named as the app settings are.
func (v githubVerification) auditFields() map[string]string {
	if v.result != githubVerificationPassed {
		return map[string]string{
			settings.KeyGitHubVerification:    v.result,
			"github_verification_skip_reason": v.skipReason,
		}
	}
	return map[string]string{
		settings.KeyGitHubVerification:  v.result,
		settings.KeyVCSRepo:             v.repo,
		settings.KeyVerifyGitCommitMode: v.mode,
		settings.KeyRequirePRForBranch:  strconv.FormatBool(v.requirePR),
		"pr_url":                        v.prURL,
	}
}

// performGitHubVerification checks GitHub verification settings and performs verification if enabled.
// It returns the verification result (with the PR URL, if the branch has an open PR), or false after writing an
// error response if verification fails or settings cannot be loaded.
func (h *APIHandler) performGitHubVerification(
	c *gin.Context,
	app string,
	req CreateDeployApprovalRequestRequest,
	gitCommitHash string,
) (githubVerification, bool) {
	if h.settingsService == nil {
		return skippedGitHubVerification("app settings are unavailable"), true
	}

	enabled, err := getAppSettingBool(h.settingsService, app, settings.KeyGitHubVerification, true)
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get github_verification setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return githubVerification{}, false
	}

	if !enabled {
		return skippedGitHubVerification("github_verification is off for the app"), true
	}

	githubRepo, err := getAppSettingString(h.settingsService, app, settings.KeyVCSRepo, "")
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get vcs_repo setting app=%s: %v", app, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
		return githubVerification{}, false
	}

	// Verification (and the PR and default-branch rules that run inside it) must not be skipped silently.
	if missing := missingGitHubVerificationConfig(h.config, githubRepo); missing != "" {
		gtwlog.Warnf("deploy approvals: github_verification is on for app=%s but %s", app, missing)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(
			"GitHub verification is on for app %s, but %s. Set it, or turn off github_verification for the app.",
			app, missing,
		)})
		return githubVerification{}, false
	}

	return h.verifyGitHubCommit(c, app, githubRepo, req, gitCommitHash)
}

// verifyGitHubCommit performs the actual GitHub commit verification.
// It validates the commit exists on the specified branch and finds the associated PR if required.
// It returns the passed verification, or false after writing an error response if verification fails.
func (h *APIHandler) verifyGitHubCommit(
	c *gin.Context,
	app string,
	githubRepo string,
	req CreateDeployApprovalRequestRequest,
	gitCommitHash string,
) (githubVerification, bool) {
	gitBranch := strings.TrimSpace(req.GitBranch)
	if gitBranch == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git_branch is required for GitHub verification"})
		return githubVerification{}, false
	}

	owner, repo := github.SplitRepo(githubRepo)
	if owner == "" || repo == "" {
		gtwlog.Warnf("deploy approvals: invalid vcs_repo format app=%s value=%s", app, githubRepo)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "GitHub integration misconfigured"})
		return githubVerification{}, false
	}

	if ok := h.checkDefaultBranchRestriction(c, app, gitBranch); !ok {
		return githubVerification{}, false
	}

	requirePR, verifyMode, ok := h.getVerificationSettings(c, app)
	if !ok {
		return githubVerification{}, false
	}

	client := github.NewClient(h.config.GitHubToken, h.config.GitHubAPIURL)
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
		return githubVerification{}, false
	}

	verification := githubVerification{
		result:    githubVerificationPassed,
		repo:      owner + "/" + repo,
		mode:      verifyMode,
		requirePR: requirePR,
		prURL:     prURL,
	}
	verification.logPassed(app, gitBranch, gitCommitHash)
	return verification, true
}

// logPassed writes one info line for a passed verification.
func (v githubVerification) logPassed(app, gitBranch, gitCommitHash string) {
	pr := v.prURL
	if pr == "" {
		pr = "none"
	}
	gtwlog.Infof(
		"deploy approvals: GitHub verification passed app=%s repo=%s branch=%s commit=%s mode=%s require_pr=%t pr=%s",
		app, v.repo, gitBranch, gitCommitHash, v.mode, v.requirePR, pr,
	)
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

// missingGitHubVerificationConfig describes what GitHub verification needs but doesn't have, or returns "".
func missingGitHubVerificationConfig(cfg *config.Config, githubRepo string) string {
	var missing []string
	if cfg == nil || strings.TrimSpace(cfg.GitHubToken) == "" {
		missing = append(missing, "the gateway has no GITHUB_TOKEN")
	}
	if strings.TrimSpace(githubRepo) == "" {
		missing = append(missing, "the app has no vcs_repo setting")
	}
	return strings.Join(missing, " and ")
}
