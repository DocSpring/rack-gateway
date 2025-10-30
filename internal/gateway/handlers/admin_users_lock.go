package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

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
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "email is required", start, nil)
		return
	}

	var req LockUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "invalid request", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "user not found", start, nil)
		return
	}

	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "unauthorized", start, nil)
		return
	}
	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "failed to load admin user", start, nil)
		return
	}

	if err := h.database.LockUser(user.ID, req.Reason, &adminUser.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, "failed to lock user", start, nil)
		return
	}

	if h.sessions != nil {
		_, _ = h.sessions.RevokeAllForUser(user.ID, &adminUser.ID)
	}

    if h.emailSender != nil {
        subject := "Account Locked"
        textBody := fmt.Sprintf("Your account has been locked by an administrator.\n\nReason: %s\n\nPlease contact your administrator for assistance.", req.Reason)
        htmlBody := fmt.Sprintf("<p>Your account has been locked by an administrator.</p><p><strong>Reason:</strong> %s</p><p>Please contact your administrator for assistance.</p>", req.Reason)
        _ = h.emailSender.Send(user.Email, subject, textBody, htmlBody)
    }

	details := map[string]interface{}{
		"reason":      req.Reason,
		"locked_by":   adminUser.Email,
		"target_user": user.Email,
	}
	h.respondAuditSuccess(c, http.StatusOK, gin.H{"message": "user locked successfully"}, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbLock), email, start, details)
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
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, "email is required", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, "user not found", start, nil)
		return
	}

	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, "unauthorized", start, nil)
		return
	}
	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, "failed to load admin user", start, nil)
		return
	}

	if err := h.database.UnlockUser(user.ID, adminUser.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, "failed to unlock user", start, nil)
		return
	}

    if h.emailSender != nil {
        subject := "Account Unlocked"
        textBody := "Your account has been unlocked by an administrator.\n\nYou can now log in again."
        htmlBody := "<p>Your account has been unlocked by an administrator.</p><p>You can now log in again.</p>"
        _ = h.emailSender.Send(user.Email, subject, textBody, htmlBody)
    }

	details := map[string]interface{}{
		"unlocked_by": adminUser.Email,
		"target_user": user.Email,
	}
	h.respondAuditSuccess(c, http.StatusOK, gin.H{"message": "user unlocked successfully"}, audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbUnlock), email, start, details)
}
