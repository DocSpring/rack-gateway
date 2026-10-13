package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// ResetUserMFA godoc
// @Summary Reset a user's MFA
// @Description Removes a user's MFA methods, backup codes and trusted devices and signs them out, so they can enroll
// @Description a new method at their next sign-in. Their API tokens are kept. An admin can't reset their own MFA.
// @Tags admin
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/mfa/reset [post]
func (h *AdminHandler) ResetUserMFA(c *gin.Context) {
	start := time.Now()
	action := audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbMFAReset)
	userEmail := strings.TrimSpace(c.Param("email"))
	if userEmail == "" {
		h.respondAuditError(c, http.StatusBadRequest, action, userEmail, "email is required", start, nil)
		return
	}

	user, adminUser, ok := h.loadUserAndAdmin(c, userEmail, start, audit.ActionVerbMFAReset)
	if !ok {
		return
	}
	if user.ID == adminUser.ID {
		h.respondAuditError(c, http.StatusBadRequest, action, userEmail,
			"you can't reset your own MFA; manage your methods on the Account Security page", start, nil)
		return
	}

	if err := h.database.ResetUserMFA(user.ID, adminUser.ID); err != nil {
		gtwlog.Errorf("users: MFA reset for %s failed: %v", user.Email, err)
		h.respondAuditError(c, http.StatusInternalServerError, action, userEmail, "failed to reset MFA", start, nil)
		return
	}

	h.sendMFAResetEmail(user.Email)

	h.respondAuditSuccess(
		c,
		http.StatusOK,
		StatusResponse{Status: "reset"},
		action,
		userEmail,
		start,
		map[string]interface{}{"reset_by": adminUser.Email, "target_user": user.Email},
	)
}

func (h *AdminHandler) sendMFAResetEmail(userEmail string) {
	if h.emailSender == nil {
		return
	}
	subject := "Multi-factor authentication reset"
	textBody := "An administrator reset your multi-factor authentication. Your MFA methods, backup codes and " +
		"trusted devices were removed, and you were signed out.\n\n" +
		"Sign in again to set up a new method. If you didn't ask for this, contact your administrator."
	htmlBody := "<p>An administrator reset your multi-factor authentication. Your MFA methods, backup codes and " +
		"trusted devices were removed, and you were signed out.</p>" +
		"<p>Sign in again to set up a new method. If you didn't ask for this, contact your administrator.</p>"
	if err := h.emailSender.Send(userEmail, subject, textBody, htmlBody); err != nil {
		gtwlog.Errorf("users: MFA reset email to %s failed: %v", userEmail, err)
	}
}
