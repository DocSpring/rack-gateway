package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// pendingMFAAllowedRoutes are the only routes a session may reach before it completes its MFA challenge.
var pendingMFAAllowedRoutes = map[string]struct{}{
	"GET /api/v1/auth/mfa/status":                     {},
	"POST /api/v1/auth/mfa/verify":                    {},
	"POST /api/v1/auth/mfa/webauthn/assertion/start":  {},
	"POST /api/v1/auth/mfa/webauthn/assertion/verify": {},
	"GET /api/v1/info":                                {},
}

// mfaFactorEnrollmentRoutes add a new MFA factor to the current user's account.
var mfaFactorEnrollmentRoutes = map[string]struct{}{
	"POST /api/v1/auth/mfa/enroll/totp/start":       {},
	"POST /api/v1/auth/mfa/enroll/totp/confirm":     {},
	"POST /api/v1/auth/mfa/enroll/yubiotp/start":    {},
	"POST /api/v1/auth/mfa/enroll/webauthn/start":   {},
	"POST /api/v1/auth/mfa/enroll/webauthn/confirm": {},
}

// RequireVerifiedMFASession blocks sessions that have not completed their MFA challenge from
// everything except the challenge endpoints, and requires an already-enrolled user to pass a
// recent MFA step-up before adding another factor. Without this, a session that has only passed
// Google login could enroll its own factor and use it to satisfy MFA.
func RequireVerifiedMFASession(
	mfaService MFAVerifier,
	database *db.Database,
	settings db.MFASettingsSource,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil || authUser.IsAPIToken || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		route := c.Request.Method + " " + c.FullPath()
		user := getUserRecord(c, database, authUser.Email)
		current, ok := currentMFASettings(c, settings)
		if !ok {
			return
		}

		if db.SessionAwaitingMFA(current, user, authUser.Session) &&
			!allowPendingMFARequest(c, route, mfaService, database, user, authUser) {
			return
		}

		if _, adding := mfaFactorEnrollmentRoutes[route]; adding && user != nil && user.MFAEnrolled {
			if !checkStepUpMFA(c, mfaService, database, settings) {
				return
			}
		}

		c.Next()
	}
}

// allowPendingMFARequest lets a session that still owes its MFA challenge reach the challenge
// endpoints, or complete the challenge inline with an MFA header. Otherwise it denies the request.
func allowPendingMFARequest(
	c *gin.Context,
	route string,
	mfaService MFAVerifier,
	database *db.Database,
	user *db.User,
	authUser *auth.User,
) bool {
	if _, ok := pendingMFAAllowedRoutes[route]; ok {
		return true
	}
	if completePendingMFAInline(c, mfaService, database, user, authUser) {
		return true
	}
	if !c.IsAborted() {
		denyStepUp(c)
	}
	return false
}

// completePendingMFAInline verifies an inline MFA credential for a session that has not yet
// completed its MFA challenge. On success the request's session counts as MFA-verified, and
// AuditBeforeActing saves that once the request's audit attempt is written.
func completePendingMFAInline(
	c *gin.Context,
	mfaService MFAVerifier,
	database *db.Database,
	user *db.User,
	authUser *auth.User,
) bool {
	if mfaService == nil || database == nil || authUser.MFAType == "" || authUser.MFAValue == "" {
		return false
	}
	if !verifyByType(c, mfaService, database, user, authUser) {
		return false
	}
	applyInlineMFA(c, authUser.Session, true)
	return true
}
