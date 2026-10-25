package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// GetAuditLog godoc
// @Summary Get an audit log entry
// @Description Returns one audit event by its ID. The audit log page opens it from a link (Slack messages link to
// @Description the events they describe).
// @Tags Audit
// @Produce json
// @Param id path int true "Audit event ID"
// @Success 200 {object} db.AuditLog
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /audit-logs/{id} [get]
func (h *AdminHandler) GetAuditLog(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audit log ID"})
		return
	}
	entry, err := h.database.GetAuditLogByID(id)
	switch {
	case errors.Is(err, db.ErrAuditLogNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "audit log not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch the audit log"})
	default:
		c.JSON(http.StatusOK, entry)
	}
}
