package middleware

import (
	"net/http"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// RequireMFAStepUp enforces a fresh MFA verification before allowing the request to proceed.
func RequireMFAStepUp(settings *db.MFASettings) gin.HandlerFunc {
	window := stepUpWindow(settings)

	return func(c *gin.Context) {
		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil {
			denyStepUp(c)
			return
		}

		if authUser.IsAPIToken {
			c.Next()
			return
		}

		session := authUser.Session
		if session == nil || session.MFAVerifiedAt == nil {
			denyStepUp(c)
			return
		}

		recent := session.RecentStepUpAt
		if recent == nil || time.Since(*recent) > window {
			denyStepUp(c)
			return
		}

		c.Next()
	}
}

func stepUpWindow(settings *db.MFASettings) time.Duration {
	if settings != nil && settings.StepUpWindowMinutes > 0 {
		return time.Duration(settings.StepUpWindowMinutes) * time.Minute
	}
	return 10 * time.Minute
}

func denyStepUp(c *gin.Context) {
	c.Header("X-MFA-Required", "step-up")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   "mfa_step_up_required",
		"message": "Multi-factor authentication is required for this action.",
	})
}
