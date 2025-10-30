package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// VerifyMFA godoc
// @Summary Verify MFA step-up
// @Description Verifies a TOTP or backup code to satisfy the MFA step-up requirement.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body VerifyMFARequest true "Verification payload"
// @Success 200 {object} VerifyMFAResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/verify [post]
func (h *AuthHandler) VerifyMFA(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	var req VerifyMFARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Use the shared verification flow with TOTP-specific verification function
	h.verifyMFAAndComplete(c, ctx, req.TrustDevice, func() (interface{}, error) {
		return h.mfaService.VerifyTOTP(ctx.userRecord, strings.TrimSpace(req.Code), ctx.ipAddress, ctx.userAgent, ctx.sessionID)
	}, func(now time.Time) {
		// Extra debug logging for TOTP step-up
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "before_update_step_up user_email=%q session_id=%d now=%q", ctx.userRecord.Email, ctx.authUser.Session.ID, now.Format(time.RFC3339))
		gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "after_update_step_up user_email=%q session_id=%d recent_step_up_at=%q", ctx.userRecord.Email, ctx.authUser.Session.ID, now.Format(time.RFC3339))
	})
}

// StartWebAuthnAssertion godoc
// @Summary Start WebAuthn assertion for MFA
// @Description Begins a WebAuthn assertion ceremony for CLI login/step-up. Returns challenge and session data.
// @Tags Auth
// @Produce json
// @Success 200 {object} WebAuthnAssertionStartResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /auth/mfa/webauthn/assertion/start [post]
func (h *AuthHandler) StartWebAuthnAssertion(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	options, sessionJSON, err := h.mfaService.StartWebAuthnAssertion(ctx.userRecord)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if ctx.authUser.Session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
		return
	}

	c.JSON(http.StatusOK, WebAuthnAssertionStartResponse{
		Options:     options,
		SessionData: string(sessionJSON),
	})
}

// VerifyWebAuthnAssertion godoc
// @Summary Verify WebAuthn assertion for MFA
// @Description Completes the WebAuthn assertion ceremony by validating the signed response.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body VerifyWebAuthnAssertionRequest true "Assertion response and session data"
// @Success 200 {object} VerifyMFAResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /auth/mfa/webauthn/assertion/verify [post]
func (h *AuthHandler) VerifyWebAuthnAssertion(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	var req VerifyWebAuthnAssertionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Use the shared verification flow with WebAuthn-specific verification function
	h.verifyMFAAndComplete(c, ctx, req.TrustDevice, func() (interface{}, error) {
		return h.mfaService.VerifyWebAuthnAssertion(ctx.userRecord, []byte(req.SessionData), []byte(req.AssertionResponse), ctx.ipAddress, ctx.userAgent, ctx.sessionID)
	}, nil) // No extra debug logging for WebAuthn
}
