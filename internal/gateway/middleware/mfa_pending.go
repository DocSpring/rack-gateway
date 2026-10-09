package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// pendingMFAInlineVerifiedKey marks a request whose inline MFA credential already completed the
// session's MFA challenge, so later MFA checks in the same request don't verify it a second time
// (which would trip TOTP replay protection).
const pendingMFAInlineVerifiedKey = "mfa_pending_inline_verified"

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
	settings *db.MFASettings,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil || authUser.IsAPIToken || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		route := c.Request.Method + " " + c.FullPath()
		user := getUserRecord(c, database, authUser.Email)

		if db.SessionAwaitingMFA(settings, user, authUser.Session) &&
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
// completed its MFA challenge, and marks the session as MFA-verified on success.
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

	session := authUser.Session
	now := time.Now()
	if err := database.UpdateSessionMFAVerified(session.ID, now, session.TrustedDeviceID); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":   "mfa_verification_record_failed",
			"message": "Failed to record MFA verification. Please try again.",
		})
		return false
	}
	session.MFAVerifiedAt = &now

	if !updateStepUpAfterSuccess(c, database, authUser) {
		return false
	}
	c.Set(pendingMFAInlineVerifiedKey, true)
	return true
}
