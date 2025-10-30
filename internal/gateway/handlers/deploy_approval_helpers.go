package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

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

// checkDeployApprovalAuth validates authentication and RBAC permissions for deploy approval operations.
// Returns the authenticated email and true on success, otherwise writes an error response and returns false.
func checkDeployApprovalAuth(c *gin.Context, rbacSvc rbac.RBACManager, action rbac.Action) (string, bool) {
	return requireAuth(c, rbacSvc, rbac.ResourceDeployApprovalRequest, action)
}

// validatePublicID ensures the path parameter `id` is present.
func validatePublicID(c *gin.Context) (string, bool) {
	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return "", false
	}
	return publicID, true
}

// loadDeployApprovalRequest retrieves a deploy approval record by public ID.
func loadDeployApprovalRequest(c *gin.Context, database *db.Database, publicID string) (*db.DeployApprovalRequest, bool) {
	record, err := database.GetDeployApprovalRequestByPublicID(publicID)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deploy approval request"})
		return nil, false
	}
	return record, true
}

// loadApprover fetches the approver user by email.
func loadApprover(c *gin.Context, database *db.Database, email string) (*db.User, bool) {
	approver, err := database.GetUser(email)
	if err != nil || approver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load approver"})
		return nil, false
	}
	return approver, true
}

// logDeployApprovalAudit wraps audit logging for deploy approval operations.
func logDeployApprovalAudit(logger *audit.Logger, userEmail, userName, action, resourceID, details, status string, httpStatus int) {
	if logger == nil {
		return
	}

	_ = logger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     userName,
		ActionType:   "gateway",
		Action:       action,
		ResourceType: "deploy-approval-request",
		Resource:     resourceID,
		Details:      details,
		Status:       status,
		RBACDecision: "allow",
		HTTPStatus:   httpStatus,
	})
}

type deployApprovalStatusInput struct {
	userEmail string
	publicID  string
	notes     string
	approver  *db.User
}

func (h *AdminHandler) ensureDeployApprovalDependencies(c *gin.Context) bool {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return false
	}
	return true
}

func (h *AdminHandler) requireDeployApprovalAccess(c *gin.Context, action rbac.Action) (string, bool) {
	if !h.ensureDeployApprovalDependencies(c) {
		return "", false
	}
	return checkDeployApprovalAuth(c, h.rbac, action)
}

func (h *AdminHandler) parseDeployApprovalStatusUpdateRequest(c *gin.Context) (*deployApprovalStatusInput, bool) {
	userEmail, ok := h.requireDeployApprovalAccess(c, rbac.ActionApprove)
	if !ok {
		return nil, false
	}

	publicID, ok := validatePublicID(c)
	if !ok {
		return nil, false
	}

	var payload UpdateDeployApprovalRequestStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return nil, false
	}

	approver, ok := loadApprover(c, h.database, userEmail)
	if !ok {
		return nil, false
	}

	return &deployApprovalStatusInput{
		userEmail: userEmail,
		publicID:  publicID,
		notes:     payload.Notes,
		approver:  approver,
	}, true
}
