package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// RequireMFA enforces MFA verification for sensitive operations like API token management
// and deploy approvals.
//
// Unlike RequireMFAStepUp, this middleware:
// - ALWAYS requires MFA code in the request (no grace period)
// - Does NOT allow API tokens to bypass MFA
//
// MFA code can be provided in two ways:
// - CLI: Inline in password as "session_token.totp.123456" or "session_token.webauthn.base64data"
// - Web: In X-MFA-Code header (e.g., "123456")
//
// The MFA code is extracted by the auth service and verified here.
func RequireMFA(mfaService *mfa.Service, database *db.Database, settings *db.MFASettings) gin.HandlerFunc {
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

		// MFA code MUST be provided with this request
		// (extracted from password or X-MFA-Code header by auth service)
		if authUser.MFAType == "" || authUser.MFAValue == "" {
			denyMFA(c)
			return
		}

		// Get user record to verify MFA
		user, err := database.GetUser(authUser.Email)
		if err != nil || user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "user_not_found",
				"message": "User not found",
			})
			return
		}

		// Verify the MFA code based on type
		var verifyErr error
		switch authUser.MFAType {
		case "totp":
			_, verifyErr = mfaService.VerifyTOTP(user, authUser.MFAValue, c.ClientIP(), c.GetHeader("User-Agent"), nil)
		case "webauthn":
			// WebAuthn inline format: base64(JSON{"session_data": "...", "assertion": {...}})
			verifyErr = verifyInlineWebAuthn(mfaService, database, user, authUser.MFAValue, c.ClientIP(), c.GetHeader("User-Agent"))
		default:
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_mfa_type",
				"message": "MFA type must be 'totp' or 'webauthn'",
			})
			return
		}

		if verifyErr != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "mfa_verification_failed",
				"message": "Invalid MFA code",
			})
			return
		}

		// MFA verified successfully
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

// verifyInlineWebAuthn decodes and verifies inline WebAuthn assertion data
func verifyInlineWebAuthn(mfaService *mfa.Service, database *db.Database, user *db.User, encodedData, ipAddress, userAgent string) error {
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
		nil, // sessionID - not needed for inline verification
	)

	return err
}
