package handlers

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/gin-gonic/gin"
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
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return
	}

	var req VerifyMFARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}

	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}

	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	var sessionID *int64
	if authUser.Session != nil {
		sessionID = &authUser.Session.ID
	}
	if _, err := h.mfaService.VerifyTOTP(userRecord, strings.TrimSpace(req.Code), ipAddress, userAgent, sessionID); err != nil {
		// Notify about failed MFA attempt
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(userRecord.Email, userRecord.Name, ipAddress, userAgent)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	var trustedDeviceID *int64
	trustedCookieSet := false

	if authUser.Session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
		return
	}

	// Only create a new trusted device if requested and session doesn't already have one
	if req.TrustDevice && (authUser.Session.TrustedDeviceID == nil || *authUser.Session.TrustedDeviceID == 0) {
		payload, err := h.mfaService.MintTrustedDevice(userRecord.ID, c.ClientIP(), c.GetHeader("User-Agent"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mint trusted device"})
			return
		}
		h.setTrustedDeviceCookie(c, payload.Token)
		trustedDeviceID = &payload.RecordID
		trustedCookieSet = true
	} else if authUser.Session.TrustedDeviceID != nil {
		// Session already has a trusted device, reuse it
		trustedDeviceID = authUser.Session.TrustedDeviceID
	}

	// Detect if this is login flow (first MFA verification) vs step-up
	isLoginFlow := authUser.Session.MFAVerifiedAt == nil

	if err := h.sessions.UpdateSessionMFAVerified(authUser.Session.ID, now, trustedDeviceID); err != nil {
		log.Printf("failed updating session mfa state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update session"})
		return
	}
	if err := h.sessions.UpdateSessionRecentStepUp(authUser.Session.ID, now); err != nil {
		log.Printf("failed updating session step-up timestamp: %v", err)
	} else if authUser.Session != nil {
		authUser.Session.RecentStepUpAt = &now
	}
	if trustedDeviceID != nil && trustedCookieSet {
		if err := h.sessions.AttachTrustedDeviceToSession(authUser.Session.ID, *trustedDeviceID); err != nil {
			log.Printf("failed attaching trusted device to session: %v", err)
		}
	}

	// Audit log for login completion after MFA verification
	if isLoginFlow && h.database != nil && h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(userRecord.Email, userRecord.Name, "web", "complete", c.ClientIP(), c.GetHeader("User-Agent"), true)
	}

	response := VerifyMFAResponse{
		MFAVerifiedAt:         now,
		RecentStepUpExpiresAt: now.Add(h.stepUpWindow()),
		TrustedDeviceCookie:   trustedCookieSet,
	}
	c.JSON(http.StatusOK, response)
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
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return
	}
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}
	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}

	options, sessionJSON, err := h.mfaService.StartWebAuthnAssertion(userRecord)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Store session data in the user's session for later verification
	if authUser.Session != nil {
		// Store WebAuthn session in the database or session store
		// For now, we'll return it to the client to send back
		c.JSON(http.StatusOK, WebAuthnAssertionStartResponse{
			Options:     options,
			SessionData: string(sessionJSON),
		})
		return
	}

	c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
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
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return
	}

	var req VerifyWebAuthnAssertionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}

	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}

	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	var sessionID *int64
	if authUser.Session != nil {
		sessionID = &authUser.Session.ID
	}
	if _, err := h.mfaService.VerifyWebAuthnAssertion(userRecord, []byte(req.SessionData), []byte(req.AssertionResponse), ipAddress, userAgent, sessionID); err != nil {
		// Notify about failed MFA attempt
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(userRecord.Email, userRecord.Name, ipAddress, userAgent)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	var trustedDeviceID *int64
	trustedCookieSet := false

	if authUser.Session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
		return
	}

	// Only create a new trusted device if requested and session doesn't already have one
	if req.TrustDevice && (authUser.Session.TrustedDeviceID == nil || *authUser.Session.TrustedDeviceID == 0) {
		payload, err := h.mfaService.MintTrustedDevice(userRecord.ID, c.ClientIP(), c.GetHeader("User-Agent"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mint trusted device"})
			return
		}
		h.setTrustedDeviceCookie(c, payload.Token)
		trustedDeviceID = &payload.RecordID
		trustedCookieSet = true
	}

	// Detect if this is login flow (first MFA verification) vs step-up
	isLoginFlow := authUser.Session.MFAVerifiedAt == nil

	// Update session with MFA verification and recent step-up timestamps
	if err := h.sessions.UpdateSessionMFAVerified(authUser.Session.ID, now, trustedDeviceID); err != nil {
		log.Printf("Warning: failed to update session MFA verified: %v", err)
	}
	if err := h.sessions.UpdateSessionRecentStepUp(authUser.Session.ID, now); err != nil {
		log.Printf("Warning: failed to update session step-up: %v", err)
	}

	// Audit log for login completion after MFA verification
	if isLoginFlow && h.database != nil && h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(userRecord.Email, userRecord.Name, "web", "complete", c.ClientIP(), c.GetHeader("User-Agent"), true)
	}

	response := VerifyMFAResponse{
		MFAVerifiedAt:         now,
		RecentStepUpExpiresAt: now.Add(h.stepUpWindow()),
		TrustedDeviceCookie:   trustedCookieSet,
	}
	c.JSON(http.StatusOK, response)
}
