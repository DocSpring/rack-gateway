package middleware

import (
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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
			message := "You must set up multi-factor authentication before you can continue using the CLI. Please run rack-gateway login and finish MFA enrollment."
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

// RequireMFAEnrollmentWeb blocks authenticated requests that rely on cookie-based sessions
// (primarily the web UI) from hitting API routes unrelated to MFA setup while enrollment is
// enforced but incomplete. This keeps every channel other than the dedicated CLI proxy locked
// down to the Account Security flows until enrollment succeeds.
func RequireMFAEnrollmentWeb(database *db.Database, settings *db.MFASettings) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil || authUser.IsAPIToken {
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

		if !shouldEnforceMFAForMiddleware(settings, user) || user.MFAEnrolled {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		if isMFAEnrollmentAllowedPath(path) {
			c.Next()
			return
		}

		c.Header("X-MFA-Required", "enrollment")
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "mfa_enrollment_required",
			"message": "Multi-factor authentication enrollment is required before you can access this resource.",
		})
		c.Abort()
	}
}

var mfaEnrollmentAllowedPrefixes = []string{
	"/.gateway/api/auth/mfa",
}

var mfaEnrollmentAllowedExact = map[string]struct{}{
	"/.gateway/api/me": {},
}

func isMFAEnrollmentAllowedPath(path string) bool {
	if _, ok := mfaEnrollmentAllowedExact[path]; ok {
		return true
	}
	for _, prefix := range mfaEnrollmentAllowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
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
