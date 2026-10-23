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

// ResetUserSignIn godoc
// @Summary Reset a user's sign-in account
// @Description A user is bound to the Google account that first signed in with their email, and another account
// @Description with that email is refused. This unbinds it, so the next account to sign in with the email is bound
// @Description to the user (for example after their Google account was re-created), and signs the user out. Their
// @Description MFA methods and API tokens are kept. An admin can't reset their own sign-in.
// @Tags admin
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/sign-in/reset [post]
func (h *AdminHandler) ResetUserSignIn(c *gin.Context) {
	start := time.Now()
	action := audit.BuildAction(rbac.ResourceUser.String(), audit.ActionVerbSignInReset)
	userEmail := strings.TrimSpace(c.Param("email"))

	user, adminUser, ok := h.loadUserAndAdmin(c, userEmail, start, audit.ActionVerbSignInReset)
	if !ok {
		return
	}
	if user.ID == adminUser.ID {
		h.respondAuditError(c, http.StatusBadRequest, action, userEmail, "you can't reset your own sign-in", start, nil)
		return
	}

	if err := h.database.ResetUserSignInAccount(user.ID, &adminUser.ID); err != nil {
		gtwlog.Errorf("users: sign-in reset for %s failed: %v", user.Email, err)
		h.respondAuditError(c, http.StatusInternalServerError, action, userEmail, "failed to reset sign-in", start, nil)
		return
	}

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
