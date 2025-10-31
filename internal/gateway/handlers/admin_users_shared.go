package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func userAuditAction(action rbac.Action) string {
	return audit.BuildAction(rbac.ResourceUser.String(), action.String())
}

func (h *AdminHandler) respondUserBadRequest(
	c *gin.Context,
	action rbac.Action,
	email string,
	start time.Time,
	message string,
) {
	h.respondAuditError(
		c,
		http.StatusBadRequest,
		userAuditAction(action),
		strings.TrimSpace(email),
		message,
		start,
		nil,
	)
}

func (h *AdminHandler) respondUserNotFound(
	c *gin.Context,
	action rbac.Action,
	email string,
	start time.Time,
) {
	h.respondAuditError(
		c,
		http.StatusNotFound,
		userAuditAction(action),
		strings.TrimSpace(email),
		"user not found",
		start,
		nil,
	)
}

func (h *AdminHandler) respondUserSuccess(
	c *gin.Context,
	status int,
	payload interface{},
	action rbac.Action,
	email string,
	start time.Time,
	details map[string]interface{},
) {
	h.respondAuditSuccess(
		c,
		status,
		payload,
		userAuditAction(action),
		h.getUserResourceID(strings.TrimSpace(email)),
		start,
		details,
	)
}
