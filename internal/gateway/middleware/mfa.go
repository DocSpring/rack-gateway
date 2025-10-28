package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// EnforceMFARequirements applies MFA policy based on the canonical HTTP route permissions map.
// It must run after authentication so the authenticated user is available on the context.
func EnforceMFARequirements(mfaService MFAVerifier, database *db.Database, settings *db.MFASettings) gin.HandlerFunc {
	return func(c *gin.Context) {
		pattern := c.FullPath()
		if pattern == "" {
			c.Next()
			return
		}

		method := c.Request.Method
		permissions, ok := rbac.HTTPMFAPermissions(method, pattern)
		if !ok {
			gtwlog.Errorf("mfa: missing MFA permission mapping for method=%s path=%s", method, pattern)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "mfa_configuration_error",
				"message": "MFA policy misconfiguration detected. Please contact an administrator.",
			})
			return
		}

		level := rbac.GetMFALevel(permissions)
		switch level {
		case rbac.MFANone:
			c.Next()
		case rbac.MFAStepUp:
			if !checkStepUpMFA(c, mfaService, database, settings) {
				return
			}
			c.Next()
		case rbac.MFAAlways:
			authUser, ok := auth.GetAuthUser(c.Request.Context())
			if !ok || authUser == nil {
				denyMFA(c)
				return
			}
			// API tokens don't have MFA
			if authUser.IsAPIToken {
				c.Next()
				return
			}
			if !verifyInlineMFA(c, mfaService, database, authUser) {
				if !c.IsAborted() {
					denyMFA(c)
				}
				return
			}
			c.Next()
		default:
			gtwlog.Errorf("mfa: unknown MFA level=%d for method=%s path=%s", level, method, pattern)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "mfa_configuration_error",
				"message": "MFA policy misconfiguration detected. Please contact an administrator.",
			})
		}
	}
}

// checkStepUpMFA verifies the user has recently completed step-up MFA.
func checkStepUpMFA(c *gin.Context, mfaService MFAVerifier, database *db.Database, settings *db.MFASettings) bool {
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil {
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "no auth user on context, method=%s path=%s", c.Request.Method, c.FullPath())
		denyStepUp(c)
		return false
	}

	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "evaluating user=%s method=%s path=%s", authUser.Email, c.Request.Method, c.FullPath())

	// API tokens bypass step-up MFA
	if authUser.IsAPIToken {
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s is API token, bypassing", authUser.Email)
		return true
	}

	session := authUser.Session
	if session == nil || session.MFAVerifiedAt == nil {
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s missing session or mfa verified timestamp", authUser.Email)
		// Check if user has MFA enrolled to determine correct error response
		user := auth.GetAuthUserRecord(c.Request.Context())
		if user == nil && database != nil {
			loadedUser, err := database.GetUser(authUser.Email)
			if err == nil && loadedUser != nil {
				user = loadedUser
			}
		}
		// If user has MFA enrolled, they just need to verify (step-up)
		// If not enrolled, they need to enroll first
		if user != nil && user.MFAEnrolled {
			gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s is enrolled but not verified, sending step-up required", authUser.Email)
			denyStepUp(c)
		} else {
			gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s not enrolled, sending enrollment required", authUser.Email)
			denyMFAEnrollment(c)
		}
		return false
	}

	window := stepUpWindow(settings)
	if recent := session.RecentStepUpAt; recent != nil && time.Since(*recent) <= window {
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s has recent step up at %s (window %s)", authUser.Email, recent.UTC().Format(time.RFC3339Nano), window)
		return true
	}

	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s no recent step up, attempting inline verification", authUser.Email)
	if verifyInlineMFA(c, mfaService, database, authUser) {
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s inline verification succeeded", authUser.Email)
		return true
	}

	if c.IsAborted() {
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s request aborted inside inline verification", authUser.Email)
		return false
	}

	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "user=%s inline verification missing or failed, denying", authUser.Email)
	denyStepUp(c)
	return false
}

// verifyInlineMFA verifies the user provided an MFA code with this request and updates step-up timestamp.
// This is used for both MFAAlways and MFAStepUp routes when MFA is provided inline.
func verifyInlineMFA(c *gin.Context, mfaService MFAVerifier, database *db.Database, authUser *auth.AuthUser) bool {
	if mfaService == nil || database == nil || authUser == nil {
		return false
	}

	session := authUser.Session
	if session == nil || session.MFAVerifiedAt == nil {
		return false
	}

	// MFA code MUST be provided with this request
	if authUser.MFAType == "" || authUser.MFAValue == "" {
		return false
	}

	// Get user record to verify MFA
	user, err := database.GetUser(authUser.Email)
	if err != nil || user == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":   "user_not_found",
			"message": "User not found",
		})
		return false
	}

	// Verify the MFA code based on type
	var verifyErr error
	sessionID := &session.ID
	switch authUser.MFAType {
	case "totp":
		_, verifyErr = mfaService.VerifyTOTP(user, authUser.MFAValue, c.ClientIP(), c.GetHeader("User-Agent"), sessionID)
	case "webauthn":
		verifyErr = verifyInlineWebAuthn(mfaService, database, user, authUser.MFAValue, c.ClientIP(), c.GetHeader("User-Agent"), sessionID)
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_mfa_type",
			"message": "MFA type must be 'totp' or 'webauthn'",
		})
		return false
	}

	if verifyErr != nil {
		message := "Verification code is incorrect or has expired. Please try again."
		if authUser.MFAType == "webauthn" {
			message = "Security key verification failed. Please try again."
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":   "mfa_verification_failed",
			"message": message,
		})
		return false
	}

	// Update step-up timestamp - any successful MFA verification refreshes the step-up window
	now := time.Now()
	if err := database.UpdateSessionRecentStepUp(session.ID, now); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":   "mfa_step_up_record_failed",
			"message": "Failed to record MFA verification. Please try again.",
		})
		return false
	}
	session.RecentStepUpAt = &now

	return true
}

func stepUpWindow(settings *db.MFASettings) time.Duration {
	if settings != nil && settings.StepUpWindowMinutes > 0 {
		return time.Duration(settings.StepUpWindowMinutes) * time.Minute
	}
	return 10 * time.Minute
}

// denyStepUp sends a step-up MFA required response.
func denyStepUp(c *gin.Context) {
	c.Header("X-MFA-Required", "step-up")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   "mfa_step_up_required",
		"message": "Multi-factor authentication is required for this action.",
	})
}

func denyMFAEnrollment(c *gin.Context) {
	c.Header("X-MFA-Required", "enrollment")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   "mfa_enrollment_required",
		"message": "Multi-factor authentication must be enabled for this account before continuing.",
	})
}

// denyMFA sends an inline MFA required response.
func denyMFA(c *gin.Context) {
	c.Header("X-MFA-Required", "always")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   "mfa_required",
		"message": "Multi-factor authentication is required for this sensitive operation.",
	})
}

// verifyInlineWebAuthn decodes and verifies inline WebAuthn assertion data
func verifyInlineWebAuthn(mfaService MFAVerifier, database *db.Database, user *db.User, encodedData, ipAddress, userAgent string, sessionID *int64) error {
	// Decode base64
	jsonData, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return err
	}

	// Parse JSON structure to extract session_data and assertion_response
	var inlineData struct {
		SessionData       string `json:"session_data"`
		AssertionResponse string `json:"assertion_response"`
	}

	if err := json.Unmarshal(jsonData, &inlineData); err != nil {
		return err
	}

	// Verify the WebAuthn assertion using the MFA service
	_, err = mfaService.VerifyWebAuthnAssertion(
		user,
		[]byte(inlineData.SessionData),
		[]byte(inlineData.AssertionResponse),
		ipAddress,
		userAgent,
		sessionID,
	)

	return err
}
