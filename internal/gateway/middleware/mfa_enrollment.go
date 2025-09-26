package middleware

import (
	"net/http"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// RequireMFAEnrollment blocks CLI sessions when MFA enforcement is active but the
// user has not yet completed enrollment. It returns a clear error so the CLI can
// instruct the user to finish setup.
func RequireMFAEnrollment(database *db.Database, settings *db.MFASettings) gin.HandlerFunc {
	return func(c *gin.Context) {
		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil || authUser.IsAPIToken {
			c.Next()
			return
		}
		session := authUser.Session
		if session == nil || !strings.EqualFold(session.Channel, "cli") {
			c.Next()
			return
		}
		if database == nil {
			c.Next()
			return
		}

		user, err := database.GetUser(authUser.Email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user profile"})
			c.Abort()
			return
		}
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
			c.Abort()
			return
		}

		if shouldEnforceMFAForMiddleware(settings, user) && !user.MFAEnrolled {
			message := "You must set up multi-factor authentication before you can continue using the CLI. Please run convox-gateway login and finish MFA enrollment."
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "mfa_enrollment_required",
				"message": message,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// handlersShouldEnforceMFA mirrors the logic used by handlers without creating a
// circular dependency (middleware cannot import handlers directly).
func shouldEnforceMFAForMiddleware(settings *db.MFASettings, user *db.User) bool {
	if user == nil {
		if settings == nil {
			return true
		}
		return settings.RequireAllUsers
	}
	if settings == nil {
		return true
	}
	if settings.RequireAllUsers {
		return true
	}
	return user.MFAEnforcedAt != nil
}
