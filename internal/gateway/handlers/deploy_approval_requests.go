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

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
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
	if h.config != nil && h.config.DeployApprovalsDisabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "deploy approvals feature is disabled"})
		return
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
	if req.CIMetadata != nil && len(req.CIMetadata) > 0 {
		var err error
		ciMetadata, err = json.Marshal(req.CIMetadata)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ci_metadata"})
			return
		}
	}

	record, err := h.database.CreateDeployApprovalRequest(
		message,
		gitCommitHash,
		req.GitBranch,
		req.PipelineURL,
		req.CIProvider,
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
		"pipeline_url":    req.PipelineURL,
		"message":         message,
	})

	if err := h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     dbUser.Name,
		ActionType:   rbac.ActionTypeGateway,
		Action:       rbac.BuildAction(rbac.ResourceStringDeployApprovalRequest, rbac.ActionStringCreate),
		ResourceType: rbac.ResourceStringDeployApprovalRequest,
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       rbac.StatusStringSuccess,
		RBACDecision: "allow",
		HTTPStatus:   http.StatusCreated,
	}); err != nil {
		// best-effort logging; ignore error
		_ = err
	}

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

	window := 15 * time.Minute
	if h.config != nil && h.config.DeployApprovalWindow > 0 {
		window = h.config.DeployApprovalWindow
	}

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

	// Trigger CircleCI approval if integration is enabled and metadata is present
	if record.CIProvider == "circleci" && len(record.CIMetadata) > 0 {
		go func() {
			if err := h.approveCircleCIJob(record); err != nil {
				fmt.Printf("Failed to approve CircleCI job for request %d: %v\n", record.ID, err)
			} else {
				fmt.Printf("Successfully approved CircleCI job for request %d\n", record.ID)
			}
		}()
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
		Action:       rbac.BuildAction(rbac.ResourceStringDeployApprovalRequest, rbac.ActionStringApprove),
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

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
		Action:       rbac.BuildAction(rbac.ResourceStringDeployApprovalRequest, rbac.ActionStringReject),
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
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
		PipelineURL:               dr.PipelineURL,
		CIProvider:                dr.CIProvider,
		App:                       dr.App,
		BuildID:                   dr.BuildID,
		ReleaseID:                 dr.ReleaseID,
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

func ptrTime(t time.Time) *time.Time {
	return &t
}

func (h *AdminHandler) approveCircleCIJob(record *db.DeployApprovalRequest) error {
	// Check if CircleCI integration is enabled
	enabled, err := h.database.CircleCIEnabled()
	if err != nil {
		return fmt.Errorf("failed to check circleci enabled: %w", err)
	}
	if !enabled {
		return fmt.Errorf("circleci integration not enabled")
	}

	// Get CircleCI settings
	settings, err := h.database.GetCircleCISettings()
	if err != nil {
		return fmt.Errorf("failed to get circleci settings: %w", err)
	}

	// Parse CI metadata
	var metadata map[string]interface{}
	if err := json.Unmarshal(record.CIMetadata, &metadata); err != nil {
		return fmt.Errorf("failed to parse ci_metadata: %w", err)
	}

	// Validate metadata
	if err := circleci.ValidateMetadata(metadata); err != nil {
		return fmt.Errorf("invalid circleci metadata: %w", err)
	}

	// Parse metadata
	approvalMeta, err := circleci.ParseMetadata(metadata)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Create CircleCI client and approve the job
	client := circleci.NewClient(settings.APIToken)
	if err := client.ApproveJob(approvalMeta.WorkflowID, approvalMeta.ApprovalJobName); err != nil {
		return fmt.Errorf("failed to approve circleci job: %w", err)
	}

	return nil
}
