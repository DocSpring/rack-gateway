package handlers

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// mfaAuditEvent describes a change to a user's MFA configuration.
type mfaAuditEvent struct {
	scope        string
	verb         string
	resourceType string
	resource     string
	details      map[string]interface{}
}

// auditMFAEvent records a change to the current user's MFA factors or backup codes in the
// database audit log.
func (h *AuthHandler) auditMFAEvent(c *gin.Context, user *db.User, event mfaAuditEvent) {
	if h.database == nil || h.auditLogger == nil || user == nil {
		return
	}
	action := audit.BuildAction(event.scope, event.verb)
	details, _ := json.Marshal(event.details)
	entry := &db.AuditLog{
		UserEmail:    user.Email,
		UserName:     user.Name,
		ActionType:   audit.ActionTypeAuth,
		Action:       action,
		ResourceType: event.resourceType,
		Resource:     event.resource,
		Details:      string(details),
		Status:       "success",
		IPAddress:    c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	}
	// The logger reports a failed write (it can't be undone: the change has been made).
	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), entry)
}
