package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// loadUserForEnrollmentCheck retrieves the user record from the database if not already loaded.
// It populates authUser.DBUser and returns the user record. Returns nil for API tokens.
func loadUserForEnrollmentCheck(ctx context.Context, database *db.Database, authUser *auth.User) (*db.User, error) {
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

// enforceMFAEnrollment runs handler for a user signed in with a session, with the current MFA settings. handler
// reports whether it refused the request.
func enforceMFAEnrollment(
	database *db.Database,
	source db.MFASettingsSource,
	handler func(*gin.Context, *db.MFASettings, *db.User) bool,
) gin.HandlerFunc {
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

		settings, ok := currentMFASettings(c, source)
		if !ok {
			return
		}
		if handler(c, settings, user) {
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireMFAEnrollment blocks the CLI proxy for a user who must use MFA but hasn't enrolled. It applies to every
// session, whatever channel it was created on: a web session's token sent as a Bearer token reaches the proxy too.
// It returns a clear error so the CLI can instruct the user to finish setup, and records the refused rack request
// (a throttled denial, see audit.Logger.RecordDenial) when logger is set: it never reaches the proxy, which records
// the others.
func RequireMFAEnrollment(database *db.Database, settings db.MFASettingsSource, logger *audit.Logger) gin.HandlerFunc {
	return enforceMFAEnrollment(database, settings, func(c *gin.Context, current *db.MFASettings, user *db.User) bool {
		if !db.ShouldEnforceMFA(current, user) || user.MFAEnrolled {
			return false
		}
		recordEnrollmentDenial(c, logger, user)
		message := "You must set up multi-factor authentication before you can continue using the CLI. " +
			"Please run rack-gateway login and finish MFA enrollment."
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "mfa_enrollment_required",
			"message": message,
		})
		return true
	})
}

// recordEnrollmentDenial records a rack request refused because its user hasn't set up MFA.
func recordEnrollmentDenial(c *gin.Context, logger *audit.Logger, user *db.User) {
	if logger == nil {
		return
	}
	path := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/rack-proxy")
	action, resource := logger.ParseConvoxAction(path, c.Request.Method, "")
	details, _ := json.Marshal(map[string]string{
		"method": c.Request.Method, "path": path, "reason": "mfa_enrollment_required",
	})
	logger.RecordDenial(&db.AuditLog{
		UserEmail:    user.Email,
		UserName:     user.Name,
		ActionType:   audit.ActionTypeConvox,
		Action:       action,
		ResourceType: logger.InferResourceType(path, action),
		Resource:     resource,
		Details:      string(details),
		HTTPStatus:   http.StatusForbidden,
		RequestID:    audit.RequestID(c.Request),
		IPAddress:    c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
}

// RequireMFAEnrollmentWeb blocks authenticated requests that rely on cookie-based sessions
// (primarily the web UI) from hitting API routes unrelated to MFA setup while enrollment is
// enforced but incomplete. This keeps every channel other than the dedicated CLI proxy locked
// down to the Account Security flows until enrollment succeeds.
func RequireMFAEnrollmentWeb(database *db.Database, settings db.MFASettingsSource) gin.HandlerFunc {
	return enforceMFAEnrollment(database, settings, func(c *gin.Context, current *db.MFASettings, user *db.User) bool {
		if c.Request.Method == http.MethodOptions {
			return false
		}
		if !db.ShouldEnforceMFA(current, user) || user.MFAEnrolled {
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
