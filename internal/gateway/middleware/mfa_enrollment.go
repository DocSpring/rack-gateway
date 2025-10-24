package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// loadUserForEnrollmentCheck retrieves the user record from the database if not already loaded.
// It populates authUser.DBUser and returns the user record. Returns nil for API tokens.
func loadUserForEnrollmentCheck(ctx context.Context, database *db.Database, authUser *auth.AuthUser) (*db.User, error) {
	if authUser == nil || authUser.IsAPIToken {
		return nil, nil
	}

	user := auth.GetAuthUserRecord(ctx)
	if user != nil {
		return user, nil
	}

	loaded, err := database.GetUser(authUser.Email)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, nil
	}

	authUser.DBUser = loaded
	return loaded, nil
}

func requireMFAEnrollment(database *db.Database, settings *db.MFASettings, handler func(*gin.Context, *auth.AuthUser, *db.User) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil || authUser.IsAPIToken {
			c.Next()
			return
		}
		if database == nil {
			c.Next()
			return
		}

		user, err := loadUserForEnrollmentCheck(c.Request.Context(), database, authUser)
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

		if handler != nil && handler(c, authUser, user) {
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireMFAEnrollment blocks CLI sessions when MFA enforcement is active but the
// user has not yet completed enrollment. It returns a clear error so the CLI can
// instruct the user to finish setup.
func RequireMFAEnrollment(database *db.Database, settings *db.MFASettings) gin.HandlerFunc {
	return requireMFAEnrollment(database, settings, func(c *gin.Context, authUser *auth.AuthUser, user *db.User) bool {
		session := authUser.Session
		if session == nil || !strings.EqualFold(session.Channel, "cli") {
			return false
		}
		if !db.ShouldEnforceMFA(settings, user) || user.MFAEnrolled {
			return false
		}
		message := "You must set up multi-factor authentication before you can continue using the CLI. Please run rack-gateway login and finish MFA enrollment."
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "mfa_enrollment_required",
			"message": message,
		})
		return true
	})
}

// RequireMFAEnrollmentWeb blocks authenticated requests that rely on cookie-based sessions
// (primarily the web UI) from hitting API routes unrelated to MFA setup while enrollment is
// enforced but incomplete. This keeps every channel other than the dedicated CLI proxy locked
// down to the Account Security flows until enrollment succeeds.
func RequireMFAEnrollmentWeb(database *db.Database, settings *db.MFASettings) gin.HandlerFunc {
	return requireMFAEnrollment(database, settings, func(c *gin.Context, authUser *auth.AuthUser, user *db.User) bool {
		if c.Request.Method == http.MethodOptions {
			return false
		}
		if !db.ShouldEnforceMFA(settings, user) || user.MFAEnrolled {
			return false
		}
		if isMFAEnrollmentAllowedPath(c.Request.URL.Path) {
			return false
		}
		c.Header("X-MFA-Required", "enrollment")
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "mfa_enrollment_required",
			"message": "Multi-factor authentication enrollment is required before you can access this resource.",
		})
		return true
	})
}

var mfaEnrollmentAllowedPrefixes = []string{
	"/api/v1/auth/mfa",
}

var mfaEnrollmentAllowedExact = map[string]struct{}{
	"/api/v1/info": {},
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
