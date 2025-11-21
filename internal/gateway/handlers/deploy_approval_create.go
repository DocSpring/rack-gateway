package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// CreateDeployApprovalRequest handles the creation of a new deploy approval request.
// It validates the request, performs GitHub verification if enabled, and creates the approval record.
func (h *APIHandler) CreateDeployApprovalRequest(c *gin.Context) {
	if !h.checkDeployApprovalDependencies(c) {
		return
	}

	if !h.checkDeployApprovalsEnabled(c) {
		return
	}

	userEmail, dbUser, ok := h.authenticateUser(c)
	if !ok {
		return
	}

	if !h.authorizeCreateRequest(c, userEmail) {
		return
	}

	var req CreateDeployApprovalRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	message, gitCommitHash, app, ok := h.validateRequestFields(c, req)
	if !ok {
		return
	}

	authUser, _ := auth.GetAuthUser(c.Request.Context())

	token, ok := h.resolveToken(c, dbUser, req, authUser)
	if !ok {
		return
	}

	targetUserID, createdByAPITokenID := h.deriveTokenIDs(token, authUser)

	ciMetadata, ok := h.marshalCIMetadata(c, req)
	if !ok {
		return
	}

	prURL, ok := h.performGitHubVerification(c, app, req, gitCommitHash)
	if !ok {
		return
	}

	record, ok := h.createApprovalRecord(
		c,
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
	if !ok {
		return
	}

	h.logApprovalCreation(userEmail, dbUser.Name, token.PublicID, gitCommitHash, req.GitBranch, message, record.ID)
	h.sendSlackNotification(c, record.ID)

	if !h.shouldPostPRComment(prURL) {
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	h.postPRCommentAsync(c, app, prURL, record.PublicID)
	c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
}

// GetDeployApprovalRequest retrieves a deploy approval request by its public ID.
// It verifies the user has permission to view the request (either owns it, owns the token, or is an admin).
func (h *APIHandler) GetDeployApprovalRequest(c *gin.Context) {
	if !h.checkDeployApprovalDependencies(c) {
		return
	}

	publicID, ok := validatePublicID(c)
	if !ok {
		return
	}

	userEmail, dbUser, ok := h.authenticateUser(c)
	if !ok {
		return
	}

	record, ok := loadDeployApprovalRequest(c, h.database, publicID)
	if !ok {
		return
	}

	if !h.authorizeViewRequest(c, userEmail, dbUser, record) {
		return
	}

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

// Helper functions for CreateDeployApprovalRequest

func (h *APIHandler) checkDeployApprovalDependencies(c *gin.Context) bool {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return false
	}
	return true
}

func (h *APIHandler) checkDeployApprovalsEnabled(c *gin.Context) bool {
	if h.settingsService == nil {
		return true
	}

	enabled, err := h.settingsService.GetDeployApprovalsEnabled()
	if err != nil {
		gtwlog.Warnf("deploy approvals: failed to get deploy_approvals_enabled setting: %v", err)
		return true
	}

	if !enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "deploy approvals feature is disabled"})
		return false
	}

	return true
}

func (h *APIHandler) authenticateUser(c *gin.Context) (string, *db.User, bool) {
	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return "", nil, false
	}

	dbUser, err := h.database.GetUser(userEmail)
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to load user email=%s: %v", userEmail, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return "", nil, false
	}
	if dbUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return "", nil, false
	}

	return userEmail, dbUser, true
}

func (h *APIHandler) authorizeCreateRequest(c *gin.Context, userEmail string) bool {
	allowed, err := h.rbac.Enforce(
		userEmail,
		rbac.ScopeGateway,
		rbac.ResourceDeployApprovalRequest,
		rbac.ActionCreate,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return false
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "you do not have permission to request a deploy approval",
		})
		return false
	}
	return true
}

func (h *APIHandler) resolveToken(
	c *gin.Context,
	dbUser *db.User,
	req CreateDeployApprovalRequestRequest,
	authUser *auth.User,
) (*db.APIToken, bool) {
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
			gtwlog.Errorf("deploy approvals: failed to resolve API token for user_id=%d: %v", dbUser.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve api token"})
		}
		return nil, false
	}
	if token == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve api token"})
		return nil, false
	}
	return token, true
}

func (_ *APIHandler) deriveTokenIDs(token *db.APIToken, authUser *auth.User) (*int64, *int64) {
	var targetUserID *int64
	if token != nil && token.UserID > 0 {
		id := token.UserID
		targetUserID = &id
	}

	var createdByAPITokenID *int64
	if authUser != nil && authUser.IsAPIToken && authUser.TokenID != nil {
		createdByAPITokenID = authUser.TokenID
	}

	return targetUserID, createdByAPITokenID
}

func (_ *APIHandler) marshalCIMetadata(c *gin.Context, req CreateDeployApprovalRequestRequest) ([]byte, bool) {
	var ciMetadata []byte
	if len(req.CIMetadata) > 0 {
		var err error
		ciMetadata, err = json.Marshal(req.CIMetadata)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ci_metadata"})
			return nil, false
		}
	}
	return ciMetadata, true
}

func (h *APIHandler) createApprovalRecord(
	c *gin.Context,
	message, app, gitCommitHash, gitBranch, prURL string,
	ciMetadata []byte,
	userID int64,
	createdByAPITokenID *int64,
	tokenID int64,
	targetUserID *int64,
) (*db.DeployApprovalRequest, bool) {
	record, err := h.database.CreateDeployApprovalRequest(
		message,
		app,
		gitCommitHash,
		gitBranch,
		prURL,
		ciMetadata,
		userID,
		createdByAPITokenID,
		tokenID,
		targetUserID,
	)
	if err != nil {
		h.handleCreateError(c, err, tokenID, gitCommitHash)
		return nil, false
	}
	return record, true
}

func (_ *APIHandler) handleCreateError(c *gin.Context, err error, tokenID int64, gitCommitHash string) {
	switch {
	case errors.Is(err, db.ErrDeployApprovalRequestActive):
		var conflict *db.DeployApprovalRequestConflictError
		if errors.As(err, &conflict) && conflict.Request != nil {
			c.JSON(http.StatusConflict, toDeployApprovalRequestResponse(conflict.Request))
			return
		}
		c.JSON(
			http.StatusConflict,
			gin.H{"error": "an approval request is already pending or approved for this token and git commit"},
		)
	default:
		gtwlog.Errorf(
			"deploy approvals: failed to create approval request for token_id=%d git_commit=%s: %v",
			tokenID,
			gitCommitHash,
			err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deploy approval request"})
	}
}

func (h *APIHandler) logApprovalCreation(
	userEmail, userName, tokenUUID, gitCommitHash, gitBranch, message string,
	recordID int64,
) {
	details := auditDetails(map[string]string{
		"token_uuid":      tokenUUID,
		"git_commit_hash": gitCommitHash,
		"git_branch":      gitBranch,
		"message":         message,
	})

	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     userName,
		ActionType:   audit.ActionTypeGateway,
		Action:       audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), rbac.ActionCreate.String()),
		ResourceType: rbac.ResourceDeployApprovalRequest.String(),
		Resource:     fmt.Sprintf("%d", recordID),
		Details:      details,
		Status:       audit.StatusSuccess,
		RBACDecision: "allow",
		HTTPStatus:   http.StatusCreated,
	})
}
