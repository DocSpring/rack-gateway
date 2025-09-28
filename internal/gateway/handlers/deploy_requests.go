package handlers

import (
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

// CreateDeployRequest godoc
// @Summary Request deploy approval
// @Description Creates a manual approval record tied to an API token.
// @Tags DeployRequests
// @Accept json
// @Produce json
// @Param request body CreateDeployRequestRequest true "Deploy request payload"
// @Success 201 {object} DeployRequestResponse
// @Failure 401 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse "Conflict - pending request already exists"
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /deploy-requests [post]
func (h *APIHandler) CreateDeployRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}
	if h.config != nil && h.config.DeployApprovalsDisabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deploy approvals are disabled"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-request", "create")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req CreateDeployRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
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

	token, err := resolveDeployRequestToken(h.database, h.rbac, dbUser, req, authUser)
	if err != nil {
		switch {
		case errors.Is(err, errDeployRequestTokenNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "api token not found"})
		case errors.Is(err, errDeployRequestForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "not authorized for target token"})
		case errors.Is(err, errDeployRequestTargetMissing):
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

	rackName := strings.TrimSpace(req.Rack)
	if rackName == "" {
		if rc, ok := h.primaryRack(); ok {
			rackName = rc.Name
			if rackName == "" {
				rackName = "default"
			}
		} else {
			rackName = "default"
		}
	}

	var targetUserID *int64
	if token != nil && token.UserID > 0 {
		id := token.UserID
		targetUserID = &id
	}

	record, err := h.database.CreateDeployRequest(rackName, message, dbUser.ID, nil, token.ID, targetUserID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrDeployRequestActive):
			c.JSON(http.StatusConflict, gin.H{"error": "an approval request is already pending or approved for this token"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deploy request"})
		}
		return
	}

	if err := audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     dbUser.Name,
		ActionType:   "gateway",
		Action:       "deploy-request.create",
		ResourceType: "deploy-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      fmt.Sprintf("token_uuid=%s,rack=%s", token.PublicID, rackName),
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusCreated,
	}); err != nil {
		// best-effort logging; ignore error
		_ = err
	}

	c.JSON(http.StatusCreated, toDeployRequestResponse(record))
}

var (
	errDeployRequestTokenNotFound = errors.New("api token not found")
	errDeployRequestForbidden     = errors.New("not authorized for target token")
	errDeployRequestTargetMissing = errors.New("target_api_token_id or target_api_token is required")
)

func resolveDeployRequestToken(database *db.Database, rbacSvc rbac.RBACManager, user *db.User, req CreateDeployRequestRequest, authUser *auth.AuthUser) (*db.APIToken, error) {
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
			return nil, errDeployRequestTokenNotFound
		}
		return nil, errDeployRequestTargetMissing
	}

	if token.UserID != user.ID {
		allowedAdmin, err := rbacSvc.Enforce(user.Email, "gateway:deploy-request", "approve")
		if err != nil {
			return nil, fmt.Errorf("failed to check admin permission: %w", err)
		}
		if !allowedAdmin {
			return nil, errDeployRequestForbidden
		}
	}

	return token, nil
}

// GetDeployRequest godoc
// @Summary Get deploy approval
// @Description Returns the status of a deploy approval request.
// @Tags DeployRequests
// @Produce json
// @Param id path int true "Deploy request ID"
// @Success 200 {object} DeployRequestResponse
// @Failure 401 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /deploy-requests/{id} [get]
func (h *APIHandler) GetDeployRequest(c *gin.Context) {
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

	record, err := h.database.GetDeployRequest(id)
	if err != nil {
		if errors.Is(err, db.ErrDeployRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deploy request"})
		return
	}

	allowedAdmin, err := h.rbac.Enforce(userEmail, "gateway:deploy-request", "approve")
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

	c.JSON(http.StatusOK, toDeployRequestResponse(record))
}

// ListDeployRequests godoc
// @Summary List deploy approvals
// @Description Lists deploy approval requests (admin).
// @Tags DeployRequests
// @Produce json
// @Param status query string false "Filter by status"
// @Param only_open query bool false "Only pending/approved"
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Success 200 {object} DeployRequestList
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-requests [get]
func (h *AdminHandler) ListDeployRequests(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-request", "approve")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	opts := db.DeployRequestListOptions{}
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

	records, err := h.database.ListDeployRequests(opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deploy requests"})
		return
	}

	responses := make([]DeployRequestResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, toDeployRequestResponse(record))
	}

	c.JSON(http.StatusOK, DeployRequestList{DeployRequests: responses})
}

// PreapproveDeployRequest godoc
// @Summary Pre-approve deploy request
// @Description Creates and immediately approves a deploy request for a target API token.
// @Tags DeployRequests
// @Accept json
// @Produce json
// @Param request body CreateDeployRequestRequest true "Deploy request payload"
// @Success 201 {object} DeployRequestResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse "Conflict - pending request already exists"
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-requests/preapprove [post]
func (h *AdminHandler) PreapproveDeploy(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-request", "approve")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req CreateDeployRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if req.TargetAPITokenID == nil || strings.TrimSpace(*req.TargetAPITokenID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_api_token_id is required"})
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
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

	targetID := strings.TrimSpace(*req.TargetAPITokenID)
	req.TargetAPITokenID = &targetID

	authUser, _ := auth.GetAuthUser(c.Request.Context())
	token, err := resolveDeployRequestToken(h.database, h.rbac, dbUser, req, authUser)
	if err != nil {
		switch {
		case errors.Is(err, errDeployRequestTokenNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "api token not found"})
		case errors.Is(err, errDeployRequestForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "not authorized for target token"})
		case errors.Is(err, errDeployRequestTargetMissing):
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

	rackName := strings.TrimSpace(req.Rack)
	if rackName == "" {
		if rc, ok := h.primaryRack(); ok {
			rackName = rc.Name
			if rackName == "" {
				rackName = "default"
			}
		} else {
			rackName = "default"
		}
	}

	var targetUserID *int64
	if token.UserID > 0 {
		id := token.UserID
		targetUserID = &id
	}

	record, err := h.database.CreateDeployRequest(rackName, message, dbUser.ID, nil, token.ID, targetUserID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrDeployRequestActive):
			c.JSON(http.StatusConflict, gin.H{"error": "an approval request is already pending or approved for this token"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deploy request"})
		}
		return
	}

	window := 15 * time.Minute
	if h.config != nil && h.config.DeployApprovalWindow > 0 {
		window = h.config.DeployApprovalWindow
	}

	expiresAt := time.Now().Add(window)
	notes := fmt.Sprintf("preapproved by %s", userEmail)

	record, err = h.database.ApproveDeployRequest(record.ID, dbUser.ID, expiresAt, notes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve deploy request"})
		return
	}

	if err := audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     dbUser.Name,
		ActionType:   "gateway",
		Action:       "deploy-request.preapprove",
		ResourceType: "deploy-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      fmt.Sprintf("token_uuid=%s,rack=%s", token.PublicID, rackName),
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusCreated,
	}); err != nil {
		_ = err
	}

	c.JSON(http.StatusCreated, toDeployRequestResponse(record))
}

// ApproveDeployRequest godoc
// @Summary Approve deploy request
// @Description Approves a pending deploy approval request.
// @Tags DeployRequests
// @Accept json
// @Produce json
// @Param id path int true "Deploy request ID"
// @Param request body UpdateDeployRequestStatusRequest false "Approval notes"
// @Success 200 {object} DeployRequestResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-requests/{id}/approve [post]
func (h *AdminHandler) ApproveDeployRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-request", "approve")
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

	var payload UpdateDeployRequestStatusRequest
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
	record, err := h.database.ApproveDeployRequest(id, approver.ID, expiresAt, payload.Notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve deploy request"})
		return
	}

	_ = audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       "deploy-request.approve",
		ResourceType: "deploy-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      fmt.Sprintf("expires_at=%s", expiresAt.UTC().Format(time.RFC3339)),
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	c.JSON(http.StatusOK, toDeployRequestResponse(record))
}

// RejectDeployRequest godoc
// @Summary Reject deploy request
// @Description Rejects a pending deploy approval request.
// @Tags DeployRequests
// @Accept json
// @Produce json
// @Param id path int true "Deploy request ID"
// @Param request body UpdateDeployRequestStatusRequest false "Rejection notes"
// @Success 200 {object} DeployRequestResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/deploy-requests/{id}/reject [post]
func (h *AdminHandler) RejectDeployRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, "gateway:deploy-request", "approve")
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

	var payload UpdateDeployRequestStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	approver, err := h.database.GetUser(userEmail)
	if err != nil || approver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load approver"})
		return
	}

	record, err := h.database.RejectDeployRequest(id, approver.ID, payload.Notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject deploy request"})
		return
	}

	_ = audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       "deploy-request.reject",
		ResourceType: "deploy-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      strings.TrimSpace(payload.Notes),
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	c.JSON(http.StatusOK, toDeployRequestResponse(record))
}

func toDeployRequestResponse(dr *db.DeployRequest) DeployRequestResponse {
	if dr == nil {
		return DeployRequestResponse{}
	}
	resp := DeployRequestResponse{
		ID:                 dr.ID,
		Rack:               dr.Rack,
		Message:            dr.Message,
		Status:             dr.Status,
		CreatedAt:          dr.CreatedAt,
		UpdatedAt:          dr.UpdatedAt,
		CreatedByEmail:     dr.CreatedByEmail,
		CreatedByName:      dr.CreatedByName,
		TargetAPITokenID:   dr.TargetAPITokenPublicID,
		TargetAPITokenName: dr.TargetAPITokenName,
		ApprovedByEmail:    dr.ApprovedByEmail,
		ApprovedByName:     dr.ApprovedByName,
		ApprovalNotes:      dr.ApprovalNotes,
		RejectedByEmail:    dr.RejectedByEmail,
		RejectedByName:     dr.RejectedByName,
		BuildID:            dr.BuildID,
		ObjectKey:          dr.ObjectKey,
		ReleaseID:          dr.ReleaseID,
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
	if dr.BuildCreatedAt != nil {
		resp.BuildCreatedAt = dr.BuildCreatedAt
	}
	if dr.ObjectCreatedAt != nil {
		resp.ObjectCreatedAt = dr.ObjectCreatedAt
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
