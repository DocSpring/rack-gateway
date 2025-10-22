package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// mfaContext holds common MFA request context
type mfaContext struct {
	authUser   *auth.AuthUser
	userRecord *db.User
	ipAddress  string
	userAgent  string
	sessionID  *int64
}

// getMFAContext extracts and validates common MFA request context.
// Returns (context, true) on success, or (nil, false) if validation fails (response already sent).
func (h *AuthHandler) getMFAContext(c *gin.Context) (*mfaContext, bool) {
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return nil, false
	}

	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return nil, false
	}

	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return nil, false
	}

	var sessionID *int64
	if authUser.Session != nil {
		sessionID = &authUser.Session.ID
	}

	return &mfaContext{
		authUser:   authUser,
		userRecord: userRecord,
		ipAddress:  c.ClientIP(),
		userAgent:  c.GetHeader("User-Agent"),
		sessionID:  sessionID,
	}, true
}

// handleTrustedDevice handles trusted device creation/reuse logic.
// Returns (trustedDeviceID, cookieWasSet, success).
func (h *AuthHandler) handleTrustedDevice(c *gin.Context, ctx *mfaContext, trustDevice bool) (*int64, bool, bool) {
	if ctx.authUser.Session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
		return nil, false, false
	}

	// Only create a new trusted device if requested and session doesn't already have one
	if trustDevice && (ctx.authUser.Session.TrustedDeviceID == nil || *ctx.authUser.Session.TrustedDeviceID == 0) {
		payload, err := h.mfaService.MintTrustedDevice(ctx.userRecord.ID, ctx.ipAddress, ctx.userAgent)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mint trusted device"})
			return nil, false, false
		}
		h.setTrustedDeviceCookie(c, payload.Token)
		return &payload.RecordID, true, true
	} else if ctx.authUser.Session.TrustedDeviceID != nil {
		// Session already has a trusted device, reuse it
		return ctx.authUser.Session.TrustedDeviceID, false, true
	}

	return nil, false, true
}

// updateSessionAfterMFA updates session timestamps after successful MFA verification.
// Returns (now, success).
func (h *AuthHandler) updateSessionAfterMFA(c *gin.Context, ctx *mfaContext, trustedDeviceID *int64, trustedCookieSet bool) (time.Time, bool) {
	if ctx.authUser.Session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
		return time.Time{}, false
	}

	now := time.Now()

	if err := h.sessions.UpdateSessionMFAVerified(ctx.authUser.Session.ID, now, trustedDeviceID); err != nil {
		log.Printf("failed updating session mfa state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update session"})
		return time.Time{}, false
	}
	ctx.authUser.Session.MFAVerifiedAt = &now
	ctx.authUser.Session.TrustedDeviceID = trustedDeviceID

	if err := h.sessions.UpdateSessionRecentStepUp(ctx.authUser.Session.ID, now); err != nil {
		log.Printf("failed updating session step-up timestamp: %v", err)
	} else if ctx.authUser.Session != nil {
		ctx.authUser.Session.RecentStepUpAt = &now
	}

	if trustedDeviceID != nil && trustedCookieSet {
		if err := h.sessions.AttachTrustedDeviceToSession(ctx.authUser.Session.ID, *trustedDeviceID); err != nil {
			log.Printf("failed attaching trusted device to session: %v", err)
		}
	}

	return now, true
}

// notifyLoginComplete sends login completion notification if this was the initial MFA verification during login.
func (h *AuthHandler) notifyLoginComplete(ctx *mfaContext, c *gin.Context, wasLoginFlow bool) {
	if wasLoginFlow && h.database != nil && h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(ctx.userRecord.Email, ctx.userRecord.Name, "web", "complete", c.ClientIP(), c.GetHeader("User-Agent"), true)
	}
}

// verifyMFAAndComplete handles the common MFA verification flow for any factor type.
// It takes a verification function that performs the specific factor verification (TOTP, WebAuthn, etc.),
// handles failure notifications, manages trusted devices, updates session state, and constructs the response.
//
// The verifyFunc should return (credential, error) where credential is the verified credential or nil on error.
// This function handles all error responses, trusted device logic, session updates, and notifications.
//
// Returns true if the verification flow completed successfully (response sent to client).
func (h *AuthHandler) verifyMFAAndComplete(
	c *gin.Context,
	ctx *mfaContext,
	trustDevice bool,
	verifyFunc func() (interface{}, error),
	extraDebugLog func(now time.Time),
) bool {
	// Perform the specific verification (TOTP, WebAuthn, etc.)
	if _, err := verifyFunc(); err != nil {
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(ctx.userRecord.Email, ctx.userRecord.Name, ctx.ipAddress, ctx.userAgent)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}

	trustedDeviceID, trustedCookieSet, ok := h.handleTrustedDevice(c, ctx, trustDevice)
	if !ok {
		return false
	}

	wasLoginFlow := ctx.authUser.Session != nil && ctx.authUser.Session.MFAVerifiedAt == nil

	now, ok := h.updateSessionAfterMFA(c, ctx, trustedDeviceID, trustedCookieSet)
	if !ok {
		return false
	}

	// Call extra debug logging if provided (for TOTP step-up)
	if extraDebugLog != nil {
		extraDebugLog(now)
	}

	h.notifyLoginComplete(ctx, c, wasLoginFlow)

	response := VerifyMFAResponse{
		MFAVerifiedAt:         now,
		RecentStepUpExpiresAt: now.Add(h.stepUpWindow()),
		TrustedDeviceCookie:   trustedCookieSet,
	}
	c.JSON(http.StatusOK, response)
	return true
}
