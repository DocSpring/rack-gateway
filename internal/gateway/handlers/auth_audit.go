package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Login and logout aren't gateway API routes, so AuditBeforeActing doesn't record them. The steps that act (create
// or revoke a session, verify MFA for a CLI login) record their attempt here before acting and their result after,
// and refuse to act when the attempt can't be written, like every other request.

// recordAuthAttempt writes the attempt entry of an auth step that's about to act, as user (email and name; empty
// before the user is known), and keeps it in the request's context for the result. It returns false when the
// entry can't be written: the step must not act, and the caller answers in the way its client understands.
func (h *AuthHandler) recordAuthAttempt(c *gin.Context, action, resource, email, name string) bool {
	ctx, err := h.auditLogger.RecordAttempt(c.Request, authAuditEntry(c, action, resource, email, name))
	if err != nil {
		return false
	}
	c.Request = c.Request.WithContext(ctx)
	return true
}

// recordAuthResult writes the outcome of an auth step whose attempt was recorded. The step has acted, so a failure
// here is only reported (the attempt is its record).
func (h *AuthHandler) recordAuthResult(c *gin.Context, action, resource, email, name, status string) {
	entry := authAuditEntry(c, action, resource, email, name)
	entry.Status = status
	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), entry)
}

func authAuditEntry(c *gin.Context, action, resource, email, name string) *db.AuditLog {
	return &db.AuditLog{
		UserEmail:    email,
		UserName:     name,
		ActionType:   audit.ActionTypeAuth,
		Action:       action,
		ResourceType: rbac.ResourceAuth.String(),
		Resource:     resource,
		IPAddress:    c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
		RBACDecision: "allow",
		EventCount:   1,
	}
}

// auditStatus is the status of a step's result entry.
func auditStatus(err error) string {
	if err != nil {
		return audit.StatusError
	}
	return audit.StatusSuccess
}
