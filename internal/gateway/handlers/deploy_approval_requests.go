package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/github"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

// CreateDeployApprovalRequest godoc
// @Summary Request deploy approval
// @Description Creates a manual approval record tied to an API token.
// @Tags DeployApprovalRequests
// @Accept json
// @Produce json
// @Param request body CreateDeployApprovalRequestRequest true "Deploy approval request payload"
// @Success 201 {object} DeployApprovalRequestResponse
// @Failure 401 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse "Conflict - pending request already exists"
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /deploy-approval-requests [post]
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
	approvalURL := fmt.Sprintf("%s/.gateway/web/deploy_approval_requests/%s", gatewayURL, record.PublicID)

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

var (
	errDeployApprovalRequestTokenNotFound = errors.New("api token not found")
	errDeployApprovalRequestForbidden     = errors.New("not authorized for target token")
	errDeployApprovalRequestTargetMissing = errors.New("target_api_token_id or target_api_token is required")
)

func resolveDeployApprovalRequestToken(database *db.Database, rbacSvc rbac.RBACManager, user *db.User, req CreateDeployApprovalRequestRequest, authUser *auth.AuthUser) (*db.APIToken, error) {
	identifier := strings.TrimSpace(req.TargetAPITokenName)
	var token *db.APIToken
	var err error

	hasExplicitTarget := false

	if req.TargetAPITokenID != nil {
		if trimmed := strings.TrimSpace(*req.TargetAPITokenID); trimmed != "" {
			token, err = database.GetAPITokenByPublicID(trimmed)
			hasExplicitTarget = true
		}
	}

	if token == nil && err == nil {
		if identifier != "" {
			hasExplicitTarget = true
			if id, parseErr := strconv.ParseInt(identifier, 10, 64); parseErr == nil {
				token, err = database.GetAPITokenByID(id)
			} else {
				token, err = database.GetAPITokenByName(identifier)
			}
		} else if authUser != nil && authUser.IsAPIToken && authUser.TokenID != nil && *authUser.TokenID > 0 {
			token, err = database.GetAPITokenByID(*authUser.TokenID)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to resolve api token: %w", err)
	}
	if token == nil {
		if hasExplicitTarget {
			return nil, errDeployApprovalRequestTokenNotFound
		}
		return nil, errDeployApprovalRequestTargetMissing
	}

	if token.UserID != user.ID {
		allowedAdmin, err := rbacSvc.Enforce(user.Email, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
		if err != nil {
			return nil, fmt.Errorf("failed to check admin permission: %w", err)
		}
		if !allowedAdmin {
			return nil, errDeployApprovalRequestForbidden
		}
	}

	return token, nil
}

// GetDeployApprovalRequest godoc
// @Summary Get deploy approval
// @Description Returns the status of a deploy approval request.
// @Tags DeployApprovalRequests
// @Produce json
// @Param id path string true "Deploy approval request public ID (UUID)"
// @Success 200 {object} DeployApprovalRequestResponse
// @Failure 401 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /deploy-approval-requests/{id} [get]
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

// ListDeployApprovalRequests godoc
// @Summary List deploy approvals
// @Description Lists deploy approval requests (admin).
// @Tags DeployApprovalRequests
// @Produce json
// @Param status query string false "Filter by status"
// @Param only_open query bool false "Only pending/approved"
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Success 200 {object} DeployApprovalRequestList
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-approval-requests [get]
func (h *AdminHandler) ListDeployApprovalRequests(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	opts := db.DeployApprovalRequestListOptions{}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		opts.Status = status
	}
	if onlyOpen := strings.TrimSpace(c.Query("only_open")); onlyOpen != "" {
		opts.OnlyOpen = onlyOpen == "true"
	}
	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			if limit < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be non-negative"})
				return
			}
			opts.Limit = limit
		}
	}
	if offsetStr := strings.TrimSpace(c.Query("offset")); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			if offset < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be non-negative"})
				return
			}
			opts.Offset = offset
		}
	}

	records, err := h.database.ListDeployApprovalRequests(opts)
	if err != nil {
		log.Printf("ERROR: failed to list deploy approval requests: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deploy approval requests"})
		return
	}

	responses := make([]DeployApprovalRequestResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, toDeployApprovalRequestResponse(record))
	}

	c.JSON(http.StatusOK, DeployApprovalRequestList{DeployApprovalRequests: responses})
}

// PreapproveDeployApprovalRequest godoc
// @Summary Pre-approve deploy approval request
// @Description Creates and immediately approves a deploy approval request for a target API token.
// @Tags DeployApprovalRequests
// @Accept json
// @Produce json
// @Param request body CreateDeployApprovalRequestRequest true "Deploy approval request payload"
// @Success 201 {object} DeployApprovalRequestResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse

// ApproveDeployApprovalRequest godoc
// @Summary Approve deploy approval request
// @Description Approves a pending deploy approval request.
// @Tags DeployApprovalRequests
// @Accept json
// @Produce json
// @Param id path string true "Deploy approval request public ID (UUID)"
// @Param request body UpdateDeployApprovalRequestStatusRequest false "Approval notes"
// @Success 200 {object} DeployApprovalRequestResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-approval-requests/{id}/approve [post]
func (h *AdminHandler) ApproveDeployApprovalRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	var payload UpdateDeployApprovalRequestStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	approver, err := h.database.GetUser(userEmail)
	if err != nil || approver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load approver"})
		return
	}

	// Get approval window from settings (default: 15 minutes)
	windowMinutes := 15
	if h.settingsService != nil {
		minutes, err := h.settingsService.GetDeployApprovalWindowMinutes()
		if err != nil {
			log.Printf("WARN: Failed to get deploy_approval_window_minutes setting: %v", err)
		} else if minutes > 0 {
			windowMinutes = minutes
		}
	}

	window := time.Duration(windowMinutes) * time.Minute
	expiresAt := time.Now().Add(window)
	record, err := h.database.ApproveDeployApprovalRequestByPublicID(publicID, approver.ID, expiresAt, payload.Notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve deploy approval request"})
		return
	}

	details := auditDetails(map[string]string{
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"notes":      strings.TrimSpace(payload.Notes),
		"message":    strings.TrimSpace(record.Message),
	})

	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       audit.BuildAction(rbac.ResourceStringDeployApprovalRequest, rbac.ActionStringApprove),
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	// Trigger CircleCI approval if configured and enabled
	if h.settingsService == nil || len(record.CIMetadata) == 0 {
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Check if CI provider is CircleCI
	ciProvider, err := getAppSettingString(h.settingsService, record.App, settings.KeyCIProvider, "")
	if err != nil {
		log.Printf("WARN: Failed to get ci_provider setting: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if ciProvider != "circleci" {
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	autoApprove, err := getAppSettingBool(h.settingsService, record.App, settings.KeyCircleCIAutoApproveOnApproval, false)
	if err != nil {
		log.Printf("WARN: Failed to get circleci_auto_approve_on_approval setting: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if !autoApprove {
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if h.config == nil || h.config.CircleCIToken == "" {
		log.Printf("WARN: CircleCI auto-approve enabled but no CircleCIToken configured")
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Parse CI metadata
	var metadata map[string]interface{}
	if err := json.Unmarshal(record.CIMetadata, &metadata); err != nil {
		log.Printf("WARN: Failed to unmarshal CircleCI metadata: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Get the approval job name from app settings
	approvalJobName, err := getAppSettingString(h.settingsService, record.App, settings.KeyCircleCIApprovalJobName, "")
	if err != nil {
		log.Printf("WARN: Failed to get circleci_approval_job_name setting: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if approvalJobName == "" {
		log.Printf("WARN: CircleCI auto-approve enabled but no approval_job_name configured for app %s", record.App)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Override approval_job_name from settings if configured
	metadata["approval_job_name"] = approvalJobName

	// Validate and parse metadata
	circleciMetadata, err := circleci.ParseMetadata(metadata)
	if err != nil {
		log.Printf("WARN: Invalid CircleCI metadata: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Trigger CircleCI approval in background (don't block response)
	go func() {
		client := circleci.NewClient(h.config.CircleCIToken)
		if err := client.ApproveJob(circleciMetadata.WorkflowID, circleciMetadata.ApprovalJobName); err != nil {
			log.Printf("ERROR: Failed to approve CircleCI job: %v", err)
		} else {
			log.Printf("INFO: Successfully approved CircleCI job %s in workflow %s", circleciMetadata.ApprovalJobName, circleciMetadata.WorkflowID)
		}
	}()

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

// RejectDeployApprovalRequest godoc
// @Summary Reject deploy approval request
// @Description Rejects a pending deploy approval request.
// @Tags DeployApprovalRequests
// @Accept json
// @Produce json
// @Param id path string true "Deploy approval request public ID (UUID)"
// @Param request body UpdateDeployApprovalRequestStatusRequest false "Rejection notes"
// @Success 200 {object} DeployApprovalRequestResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-approval-requests/{id}/reject [post]
func (h *AdminHandler) RejectDeployApprovalRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	var payload UpdateDeployApprovalRequestStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	approver, err := h.database.GetUser(userEmail)
	if err != nil || approver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load approver"})
		return
	}

	record, err := h.database.RejectDeployApprovalRequestByPublicID(publicID, approver.ID, payload.Notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject deploy approval request"})
		return
	}

	details := auditDetails(map[string]string{
		"notes":   strings.TrimSpace(payload.Notes),
		"message": strings.TrimSpace(record.Message),
	})

	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       audit.BuildAction(rbac.ResourceStringDeployApprovalRequest, audit.ActionVerbReject),
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

// GetDeployApprovalRequestAuditLogs godoc
// @Summary Get audit logs for deploy approval request
// @Description Returns audit logs associated with a specific deploy approval request.
// @Tags DeployApprovalRequests
// @Produce json
// @Param id path string true "Deploy approval request public ID (UUID)"
// @Param limit query int false "Limit (default 100)"
// @Success 200 {object} AuditLogsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-approval-requests/{id}/audit-logs [get]
func (h *AdminHandler) GetDeployApprovalRequestAuditLogs(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	// Get the deploy approval request to verify it exists and get the internal ID
	record, err := h.database.GetDeployApprovalRequestByPublicID(publicID)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deploy approval request"})
		return
	}

	limit := 100
	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	logs, err := h.database.GetAuditLogsByDeployApprovalRequestID(record.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch audit logs"})
		return
	}

	c.JSON(http.StatusOK, AuditLogsResponse{
		Logs:  logs,
		Total: len(logs),
		Page:  1,
		Limit: limit,
	})
}

func toDeployApprovalRequestResponse(dr *db.DeployApprovalRequest) DeployApprovalRequestResponse {
	if dr == nil {
		return DeployApprovalRequestResponse{}
	}
	resp := DeployApprovalRequestResponse{
		PublicID:                  dr.PublicID,
		Message:                   dr.Message,
		Status:                    dr.Status,
		CreatedAt:                 dr.CreatedAt,
		UpdatedAt:                 dr.UpdatedAt,
		CreatedByEmail:            dr.CreatedByEmail,
		CreatedByName:             dr.CreatedByName,
		CreatedByAPITokenPublicID: dr.CreatedByAPITokenPublicID,
		CreatedByAPITokenName:     dr.CreatedByAPITokenName,
		TargetAPITokenID:          dr.TargetAPITokenPublicID,
		TargetAPITokenName:        dr.TargetAPITokenName,
		ApprovedByEmail:           dr.ApprovedByEmail,
		ApprovedByName:            dr.ApprovedByName,
		ApprovalNotes:             dr.ApprovalNotes,
		RejectedByEmail:           dr.RejectedByEmail,
		RejectedByName:            dr.RejectedByName,
		GitCommitHash:             dr.GitCommitHash,
		GitBranch:                 dr.GitBranch,
		PrURL:                     dr.PrURL,
		App:                       dr.App,
		ObjectURL:                 dr.ObjectURL,
		BuildID:                   dr.BuildID,
		ReleaseID:                 dr.ReleaseID,
		ProcessIDs:                dr.ProcessIDs,
	}
	// Unmarshal exec commands
	if len(dr.ExecCommands) > 0 {
		var commands map[string]interface{}
		if err := json.Unmarshal(dr.ExecCommands, &commands); err == nil {
			resp.ExecCommands = commands
		}
	}
	// Unmarshal CI metadata
	if len(dr.CIMetadata) > 0 {
		var metadata map[string]interface{}
		if err := json.Unmarshal(dr.CIMetadata, &metadata); err == nil {
			resp.CIMetadata = metadata
		}
	}
	if dr.ApprovedAt != nil {
		resp.ApprovedAt = dr.ApprovedAt
	}
	if dr.ApprovalExpiresAt != nil {
		resp.ApprovalExpiresAt = dr.ApprovalExpiresAt
	}
	if dr.RejectedAt != nil {
		resp.RejectedAt = dr.RejectedAt
	}
	if dr.ReleaseCreatedAt != nil {
		resp.ReleaseCreatedAt = dr.ReleaseCreatedAt
	}
	if dr.ReleasePromotedAt != nil {
		resp.ReleasePromotedAt = dr.ReleasePromotedAt
	}
	if dr.ReleasePromotedByAPITokenID != nil {
		resp.ReleasePromotedByTokenID = dr.ReleasePromotedByAPITokenID
	}
	return resp
}

func auditDetails(values map[string]string) string {
	filtered := make(map[string]string)
	for key, value := range values {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return "{}"
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// getAppSettingBool retrieves a boolean app setting
func getAppSettingBool(svc *settings.Service, appName, key string, defaultValue bool) (bool, error) {
	setting, err := svc.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		return defaultValue, err
	}
	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}
	return defaultValue, nil
}

// getAppSettingString retrieves a string app setting
func getAppSettingString(svc *settings.Service, appName, key string, defaultValue string) (string, error) {
	setting, err := svc.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		return defaultValue, err
	}
	if val, ok := setting.Value.(string); ok {
		return val, nil
	}
	return defaultValue, nil
}
