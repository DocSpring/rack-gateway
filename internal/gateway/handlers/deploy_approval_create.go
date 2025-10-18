package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/github"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

func (h *APIHandler) CreateDeployApprovalRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	// Check if deploy approvals are enabled (default: true)
	if h.settingsService != nil {
		enabled, err := h.settingsService.GetDeployApprovalsEnabled()
		if err != nil {
			log.Printf("WARN: Failed to get deploy_approvals_enabled setting: %v", err)
		} else if !enabled {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approvals feature is disabled"})
			return
		}
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionCreate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "you do not have permission to request a deploy approval"})
		return
	}

	var req CreateDeployApprovalRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	gitCommitHash := strings.TrimSpace(req.GitCommitHash)
	if gitCommitHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git_commit_hash is required"})
		return
	}

	app := strings.TrimSpace(req.App)
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app is required"})
		return
	}

	dbUser, err := h.database.GetUser(userEmail)
	if err != nil {
		fmt.Printf("CreateDeployApprovalRequest: Failed to load user %s: %v\n", userEmail, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if dbUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	authUser, _ := auth.GetAuthUser(c.Request.Context())

	token, err := resolveDeployApprovalRequestToken(h.database, h.rbac, dbUser, req, authUser)
	if err != nil {
		switch {
		case errors.Is(err, errDeployApprovalRequestTokenNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "api token not found"})
		case errors.Is(err, errDeployApprovalRequestForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "not authorized for target token"})
		case errors.Is(err, errDeployApprovalRequestTargetMissing):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			fmt.Printf("CreateDeployApprovalRequest: Failed to resolve API token: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve api token"})
		}
		return
	}
	if token == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve api token"})
		return
	}

	var targetUserID *int64
	if token != nil && token.UserID > 0 {
		id := token.UserID
		targetUserID = &id
	}

	var createdByAPITokenID *int64
	if authUser != nil && authUser.IsAPIToken && authUser.TokenID != nil {
		createdByAPITokenID = authUser.TokenID
	}

	// Marshal CI metadata to JSON bytes
	var ciMetadata []byte
	if len(req.CIMetadata) > 0 {
		var err error
		ciMetadata, err = json.Marshal(req.CIMetadata)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ci_metadata"})
			return
		}
	}

	// GitHub verification based on app settings
	var prURL string
	if h.settingsService != nil {
		if githubVerificationEnabled, err := getAppSettingBool(h.settingsService, app, settings.KeyGitHubVerification, true); err != nil {
			fmt.Printf("CreateDeployApprovalRequest: Failed to get github_verification setting: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
			return
		} else if githubVerificationEnabled && h.config != nil && h.config.GitHubToken != "" {
			if githubRepo, err := getAppSettingString(h.settingsService, app, settings.KeyVCSRepo, ""); err != nil {
				fmt.Printf("CreateDeployApprovalRequest: Failed to get vcs_repo setting: %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
				return
			} else if githubRepo != "" {
				gitBranch := strings.TrimSpace(req.GitBranch)
				if gitBranch == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "git_branch is required for GitHub verification"})
					return
				}

				owner, repo := github.SplitRepo(githubRepo)
				if owner == "" || repo == "" {
					fmt.Printf("CreateDeployApprovalRequest: Invalid vcs_repo format: %s (expected owner/repo)\n", githubRepo)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "GitHub integration misconfigured"})
					return
				}

				// Check if deploying from default branch is allowed
				allowDefaultBranch, err := getAppSettingBool(h.settingsService, app, settings.KeyAllowDeployFromDefaultBranch, false)
				if err != nil {
					fmt.Printf("CreateDeployApprovalRequest: Failed to get allow_deploy_from_default_branch setting: %v\n", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
					return
				}

				defaultBranch, err := getAppSettingString(h.settingsService, app, settings.KeyDefaultBranch, "main")
				if err != nil {
					fmt.Printf("CreateDeployApprovalRequest: Failed to get default_branch setting: %v\n", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
					return
				}

				if !allowDefaultBranch && gitBranch == defaultBranch {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("deploying from default branch '%s' is not allowed", defaultBranch)})
					return
				}

				// Get verification mode and PR requirement
				requirePR, err := getAppSettingBool(h.settingsService, app, settings.KeyRequirePRForBranch, true)
				if err != nil {
					fmt.Printf("CreateDeployApprovalRequest: Failed to get require_pr_for_branch setting: %v\n", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
					return
				}

				verifyMode, err := getAppSettingString(h.settingsService, app, settings.KeyVerifyGitCommitMode, settings.VerifyGitCommitModeLatest)
				if err != nil {
					fmt.Printf("CreateDeployApprovalRequest: Failed to get verify_git_commit_mode setting: %v\n", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load app settings"})
					return
				}

				client := github.NewClient(h.config.GitHubToken)
				opts := github.VerifyCommitOptions{
					RequirePR: requirePR,
					Mode:      verifyMode,
				}

				prURL, err = client.VerifyCommitAndFindPR(owner, repo, gitBranch, gitCommitHash, opts)
				if err != nil {
					fmt.Printf("CreateDeployApprovalRequest: GitHub verification failed: %v\n", err)
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("GitHub verification failed: %s", err.Error())})
					return
				}
			}
		}
	}

	record, err := h.database.CreateDeployApprovalRequest(
		message,
		app,
		gitCommitHash,
		req.GitBranch,
		prURL,
		ciMetadata,
		dbUser.ID,
		createdByAPITokenID,
		token.ID,
		targetUserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrDeployApprovalRequestActive):
			var conflict *db.DeployApprovalRequestConflictError
			if errors.As(err, &conflict) && conflict.Request != nil {
				c.JSON(http.StatusConflict, toDeployApprovalRequestResponse(conflict.Request))
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "an approval request is already pending or approved for this token and git commit"})
		default:
			fmt.Printf("CreateDeployApprovalRequest: Failed to create deploy approval request: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deploy approval request"})
		}
		return
	}

	details := auditDetails(map[string]string{
		"token_uuid":      token.PublicID,
		"git_commit_hash": gitCommitHash,
		"git_branch":      req.GitBranch,
		"message":         message,
	})

	if err := h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     dbUser.Name,
		ActionType:   audit.ActionTypeGateway,
		Action:       audit.BuildAction(rbac.ResourceStringDeployApprovalRequest, rbac.ActionStringCreate),
		ResourceType: rbac.ResourceStringDeployApprovalRequest,
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       audit.StatusSuccess,
		RBACDecision: "allow",
		HTTPStatus:   http.StatusCreated,
	}); err != nil {
		// best-effort logging; ignore error
		_ = err
	}

	// Post PR comment if GitHub integration is enabled and PR was found
	if h.settingsService == nil || prURL == "" || h.config == nil || h.config.GitHubToken == "" {
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	postComment, err := getAppSettingBool(h.settingsService, app, settings.KeyGitHubPostPRComment, true)
	if err != nil {
		log.Printf("WARN: Failed to get github_post_pr_comment setting: %v", err)
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	if !postComment {
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	githubRepo, err := getAppSettingString(h.settingsService, app, settings.KeyVCSRepo, "")
	if err != nil {
		log.Printf("WARN: Failed to get vcs_repo setting: %v", err)
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	if githubRepo == "" {
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	// Build the deploy approval request URL
	gatewayURL := h.config.Domain
	if gatewayURL == "" || gatewayURL == "localhost" {
		gatewayURL = fmt.Sprintf("http://localhost:%s", h.config.Port)
	} else {
		gatewayURL = fmt.Sprintf("https://%s", gatewayURL)
	}
	approvalURL := fmt.Sprintf("%s/app/deploy_approval_requests/%s", gatewayURL, record.PublicID)

	// Extract PR number from URL
	prNumber, err := github.ExtractPRNumber(prURL)
	if err != nil {
		log.Printf("WARN: Failed to extract PR number from URL %s: %v", prURL, err)
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	owner, repo := github.SplitRepo(githubRepo)
	if owner == "" || repo == "" {
		log.Printf("WARN: Invalid vcs_repo format: %s", githubRepo)
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	comment := fmt.Sprintf("## Deploy Approval Request\n\nA deploy approval request has been created for this PR.\n\n**View request:** %s", approvalURL)

	// Post comment in background (don't block response)
	go func() {
		client := github.NewClient(h.config.GitHubToken)
		if err := client.PostPRComment(owner, repo, prNumber, comment); err != nil {
			log.Printf("ERROR: Failed to post PR comment: %v", err)
		} else {
			log.Printf("INFO: Successfully posted comment on PR #%d", prNumber)
		}
	}()

	c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
}

func (h *APIHandler) GetDeployApprovalRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	dbUser, err := h.database.GetUser(userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if dbUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	record, err := h.database.GetDeployApprovalRequestByPublicID(publicID)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deploy approval request"})
		return
	}

	allowedAdmin, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}

	ownsRequest := record.CreatedByUserID != nil && *record.CreatedByUserID == dbUser.ID
	ownsToken := record.TargetUserID != nil && *record.TargetUserID == dbUser.ID

	if !allowedAdmin && !ownsRequest && !ownsToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}
