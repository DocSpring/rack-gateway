package middleware

import (
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// RequireMFA enforces MFA for sensitive operations like API token management.
// Unlike RequireMFAStepUp, this middleware:
// - ALWAYS requires MFA (no grace period)
// - Does NOT allow API tokens to bypass MFA
// - Requires fresh MFA verification for every request
//
// Use this for highly sensitive operations where we want to ensure the actual
// human user is present and authenticated, not just an API token.
func RequireMFA(settings *db.MFASettings) gin.HandlerFunc {
	return func(c *gin.Context) {
		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil {
			denyMFA(c)
			return
		}

		// CRITICAL: Do NOT allow API tokens to bypass MFA for sensitive operations
		// API tokens are credentials that can be stolen/leaked, so we always require
		// the actual human user to re-authenticate with MFA
		if authUser.IsAPIToken {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "api_token_not_allowed",
				"message": "API tokens cannot be used for this operation. Please authenticate with your user session.",
			})
			return
		}

		session := authUser.Session
		if session == nil || session.MFAVerifiedAt == nil {
			denyMFA(c)
			return
		}

		// CRITICAL: Always check RecentStepUpAt, even with zero grace period
		// This ensures the user performed MFA step-up specifically for this request
		if session.RecentStepUpAt == nil {
			denyMFA(c)
			return
		}

		c.Next()
	}
}

func denyMFA(c *gin.Context) {
	c.Header("X-MFA-Required", "always")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   "mfa_required",
		"message": "Multi-factor authentication is required for this sensitive operation.",
	})
}
