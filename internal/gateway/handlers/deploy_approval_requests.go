package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
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
		// When deploy approvals are disabled, return an auto-approved response
		// so CI pipelines can use the same code across all environments
		c.JSON(http.StatusCreated, DeployApprovalRequestResponse{
			ID:                0,
			Message:           "deploy approvals disabled - auto-approved",
			Status:            "approved",
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
			ApprovedAt:        ptrTime(time.Now()),
			ApprovalExpiresAt: ptrTime(time.Now().Add(24 * time.Hour)),
			ApprovalNotes:     "deploy approvals feature disabled",
		})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-approval-request", "create")
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

	app := strings.TrimSpace(req.App)
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app is required"})
		return
	}

	releaseID := strings.TrimSpace(req.ReleaseID)
	if releaseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "release_id is required"})
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

	record, err := h.database.CreateDeployApprovalRequest(message, app, releaseID, dbUser.ID, createdByAPITokenID, token.ID, targetUserID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrDeployApprovalRequestActive):
			var conflict *db.DeployApprovalRequestConflictError
			if errors.As(err, &conflict) && conflict.Request != nil {
				c.JSON(http.StatusConflict, toDeployApprovalRequestResponse(conflict.Request))
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "an approval request is already pending or approved for this token and release"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deploy approval request"})
		}
		return
	}

	details := auditDetails(map[string]string{
		"token_uuid": token.PublicID,
		"app":        app,
		"release_id": releaseID,
		"message":    message,
	})

	if err := audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     dbUser.Name,
		ActionType:   "gateway",
		Action:       "deploy-approval-request.create",
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
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
		allowedAdmin, err := rbacSvc.Enforce(user.Email, "gateway:deploy-approval-request", "approve")
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
// @Param id path int true "Deploy approval request ID"
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

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
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

	record, err := h.database.GetDeployApprovalRequest(id)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deploy approval request"})
		return
	}

	allowedAdmin, err := h.rbac.Enforce(userEmail, "gateway:deploy-approval-request", "approve")
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
	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-approval-request", "approve")
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
// @Param id path int true "Deploy approval request ID"
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

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-approval-request", "approve")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
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
	record, err := h.database.ApproveDeployApprovalRequest(id, approver.ID, expiresAt, payload.Notes)
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

	_ = audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       "deploy-approval-request.approve",
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
// @Param id path int true "Deploy approval request ID"
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

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-approval-request", "approve")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
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

	record, err := h.database.RejectDeployApprovalRequest(id, approver.ID, payload.Notes)
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

	_ = audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       "deploy-approval-request.reject",
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
		ID:                        dr.ID,
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
		App:                       dr.App,
		ReleaseID:                 dr.ReleaseID,
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
