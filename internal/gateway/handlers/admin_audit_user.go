package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ListUserAuditLogs godoc
// @Summary List a user's audit logs
// @Description Returns paginated audit logs for actions performed by one user. Every user may read
// @Description their own activity; reading another user's activity requires gateway:audit_log:read.
// @Tags Audit
// @Produce json
// @Param email path string true "User email"
// @Param search query string false "Text search"
// @Param action_type query string false "Action type filter"
// @Param resource_type query string false "Resource type filter"
// @Param status query string false "Status filter"
// @Param page query int false "Page number"
// @Param limit query int false "Page size"
// @Param start query string false "ISO8601 start time"
// @Param end query string false "ISO8601 end time"
// @Param range query string false "Relative range (e.g. 24h, 7d, custom)"
// @Success 200 {object} AuditLogsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /users/{email}/audit-logs [get]
func (h *AdminHandler) ListUserAuditLogs(c *gin.Context) {
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "audit.list", email, "email is required", time.Now(), nil)
		return
	}
	h.respondWithAuditLogs(c, email)
}
