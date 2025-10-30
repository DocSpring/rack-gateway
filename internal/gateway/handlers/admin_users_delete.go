package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// DeleteUser godoc
// @Summary Delete a user
// @Description Removes a gateway user and revokes all sessions they own.
// @Tags Users
// @Param email path string true "User email"
// @Success 204 {string} string "No Content"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email} [delete]
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	start := time.Now()
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	if email == currentUser {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionDelete.String()),
			strings.TrimSpace(email),
			"cannot delete yourself",
			start,
			nil,
		)
		return
	}

	if err := h.rbac.DeleteUser(email); err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionDelete.String()),
			strings.TrimSpace(email),
			"failed to delete user",
			start,
			nil,
		)
		return
	}

	h.respondAuditSuccess(
		c,
		http.StatusNoContent,
		nil,
		audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionDelete.String()),
		strings.TrimSpace(email),
		start,
		nil,
	)
}
