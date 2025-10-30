package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// LockUserRequest contains the reason for locking a user account.
type LockUserRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// LockUser godoc
// @Summary Lock a user account
// @Description Locks a user account to prevent login
// @Tags admin
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Param request body LockUserRequest true "Lock reason"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/lock [post]
func (h *AdminHandler) LockUser(c *gin.Context) {
	start := time.Now()
	userEmail := strings.TrimSpace(c.Param("email"))
	if userEmail == "" {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock),
			userEmail,
			"email is required",
			start,
			nil,
		)
		return
	}

	var req LockUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock),
			userEmail,
			"invalid request",
			start,
			nil,
		)
		return
	}

	user, adminUser, ok := h.validateLockUnlockUsers(c, userEmail, start, audit.ActionVerbLock)
	if !ok {
		return
	}

	if err := h.database.LockUser(user.ID, req.Reason, &adminUser.ID); err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock),
			userEmail,
			"failed to lock user",
			start,
			nil,
		)
		return
	}

	if h.sessions != nil {
		_, _ = h.sessions.RevokeAllForUser(user.ID, &adminUser.ID)
	}

	h.sendLockEmail(user.Email, req.Reason)

	details := map[string]interface{}{
		"reason":      req.Reason,
		"locked_by":   adminUser.Email,
		"target_user": user.Email,
	}
	h.respondAuditSuccess(
		c,
		http.StatusOK,
		gin.H{"message": "user locked successfully"},
		audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock),
		userEmail,
		start,
		details,
	)
}

// UnlockUser godoc
// @Summary Unlock a user account
// @Description Unlocks a previously locked user account
// @Tags admin
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/unlock [post]
func (h *AdminHandler) UnlockUser(c *gin.Context) {
	start := time.Now()
	userEmail := strings.TrimSpace(c.Param("email"))
	if userEmail == "" {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock),
			userEmail,
			"email is required",
			start,
			nil,
		)
		return
	}

	user, adminUser, ok := h.validateLockUnlockUsers(c, userEmail, start, audit.ActionVerbUnlock)
	if !ok {
		return
	}

	if err := h.database.UnlockUser(user.ID, adminUser.ID); err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock),
			userEmail,
			"failed to unlock user",
			start,
			nil,
		)
		return
	}

	h.sendUnlockEmail(user.Email)

	details := map[string]interface{}{
		"unlocked_by": adminUser.Email,
		"target_user": user.Email,
	}
	h.respondAuditSuccess(
		c,
		http.StatusOK,
		gin.H{"message": "user unlocked successfully"},
		audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock),
		userEmail,
		start,
		details,
	)
}

func (h *AdminHandler) validateLockUnlockUsers(
	c *gin.Context,
	userEmail string,
	start time.Time,
	actionVerb string,
) (*db.User, *db.User, bool) {
	action := audit.BuildAction(rbac.ResourceUser.String(), actionVerb)

	user, err := h.database.GetUser(userEmail)
	if err != nil {
		h.respondAuditError(
			c, http.StatusInternalServerError, action, userEmail, "failed to load user", start, nil,
		)
		return nil, nil, false
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, action, userEmail, "user not found", start, nil)
		return nil, nil, false
	}

	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, action, userEmail, "unauthorized", start, nil)
		return nil, nil, false
	}

	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(
			c, http.StatusInternalServerError, action, userEmail, "failed to load admin user", start, nil,
		)
		return nil, nil, false
	}

	return user, adminUser, true
}

func (h *AdminHandler) sendLockEmail(userEmail, reason string) {
	if h.emailSender == nil {
		return
	}
	subject := "Account Locked"
	textBody := fmt.Sprintf(
		"Your account has been locked by an administrator.\n\n"+
			"Reason: %s\n\n"+
			"Please contact your administrator for assistance.",
		reason,
	)
	htmlBody := fmt.Sprintf(
		"<p>Your account has been locked by an administrator.</p>"+
			"<p><strong>Reason:</strong> %s</p>"+
			"<p>Please contact your administrator for assistance.</p>",
		reason,
	)
	_ = h.emailSender.Send(userEmail, subject, textBody, htmlBody)
}

func (h *AdminHandler) sendUnlockEmail(userEmail string) {
	if h.emailSender == nil {
		return
	}
	subject := "Account Unlocked"
	textBody := "Your account has been unlocked by an administrator.\n\nYou can now log in again."
	htmlBody := "<p>Your account has been unlocked by an administrator.</p>" +
		"<p>You can now log in again.</p>"
	_ = h.emailSender.Send(userEmail, subject, textBody, htmlBody)
}
