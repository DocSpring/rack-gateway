package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs/deployapprovals"
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

	if !h.authorizeCreateRequest(c) {
		return
	}

	if !h.admitApprovalAttempt(c, dbUser.ID) {
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

	_, createdByAPITokenID := h.deriveTokenIDs(token, authUser)

	ciMetadata, ok := h.marshalCIMetadata(c, req)
	if !ok {
		return
	}

	if !h.admitApprovalCreation(c, token.ID, app, gitCommitHash, dbUser.ID) {
		return
	}

	verification, ok := h.performGitHubVerification(c, app, req, gitCommitHash)
	if !ok {
		return
	}

	record, ok := h.createApprovalRecord(
		c,
		message,
		app,
		gitCommitHash,
		req.GitBranch,
		verification.prURL,
		ciMetadata,
		dbUser.ID,
		createdByAPITokenID,
		token.ID,
	)
	if !ok {
		return
	}

	h.logApprovalCreation(c.Request.Context(), userEmail, dbUser.Name, token.PublicID, record, verification)
	h.sendSlackNotification(c, record.ID)

	if !h.shouldPostPRComment(verification.prURL) {
		c.JSON(http.StatusCreated, toDeployApprovalRequestResponse(record))
		return
	}

	h.postPRCommentAsync(c, app, verification.prURL, record.PublicID, record.GitCommitHash)
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

	_, dbUser, ok := h.authenticateUser(c)
	if !ok {
		return
	}

	record, ok := loadDeployApprovalRequest(c, h.database, publicID)
	if !ok {
		return
	}

	if !h.authorizeViewRequest(c, dbUser, record) {
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

func (h *APIHandler) authorizeCreateRequest(c *gin.Context) bool {
	if !callerCan(c, h.rbac, rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionCreate)) {
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
) (*db.DeployApprovalRequest, bool) {
	// admitApprovalCreation has expired the approvals past their window.
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
			gin.H{"error": "an approval request is already pending or approved for this app, token and git commit"},
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

// logApprovalCreation audit-logs a new request, with what GitHub verification did for it. ctx is the HTTP
// request's context, so the entry is linked to its attempt.
func (h *APIHandler) logApprovalCreation(
	ctx context.Context,
	userEmail, userName, tokenUUID string,
	record *db.DeployApprovalRequest,
	verification githubVerification,
) {
	fields := verification.auditFields()
	fields["token_uuid"] = tokenUUID
	fields["git_commit_hash"] = record.GitCommitHash
	fields["git_branch"] = record.GitBranch
	fields["message"] = strings.TrimSpace(record.Message)
	details := auditDetails(fields)

	action := audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), rbac.ActionCreate.String())
	_ = h.auditLogger.LogRequestEntry(ctx, &db.AuditLog{
		UserEmail:               userEmail,
		UserName:                userName,
		ActionType:              audit.ActionTypeGateway,
		Action:                  action,
		ResourceType:            rbac.ResourceDeployApprovalRequest.String(),
		Resource:                fmt.Sprintf("%d", record.ID),
		Details:                 details,
		Status:                  audit.StatusSuccess,
		RBACDecision:            "allow",
		HTTPStatus:              http.StatusCreated,
		DeployApprovalRequestID: &record.ID,
	})
}

// expireDeployApprovals runs the expiry sweep, as the periodic job does.
func (h *APIHandler) expireDeployApprovals() error {
	var pendingMaxAge time.Duration
	if h.config != nil {
		pendingMaxAge = h.config.DeployApprovalPendingMaxAge
	}
	return deployapprovals.Expire(h.database, pendingMaxAge)
}
