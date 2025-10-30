package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// StartTOTPEnrollment godoc
// @Summary Start TOTP enrollment
// @Description Generates a TOTP secret, provisioning URI, and backup codes for the authenticated user.
// @Tags Auth
// @Produce json
// @Success 200 {object} StartTOTPEnrollmentResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/enroll/totp/start [post]
func (h *AuthHandler) StartTOTPEnrollment(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	result, err := h.mfaService.StartTOTPEnrollment(ctx.userRecord)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := StartTOTPEnrollmentResponse{
		MethodID:    result.MethodID,
		Secret:      result.Secret,
		URI:         result.URI,
		BackupCodes: result.BackupCodes,
	}
	c.JSON(http.StatusOK, response)
}

// ConfirmTOTPEnrollment godoc
// @Summary Confirm TOTP enrollment
// @Description Confirms the TOTP secret using a verification code and optionally trusts the device.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ConfirmTOTPEnrollmentRequest true "Enrollment confirmation payload"
// @Success 200 {object} VerifyMFAResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/enroll/totp/confirm [post]
func (h *AuthHandler) ConfirmTOTPEnrollment(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	var req ConfirmTOTPEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.MethodID == 0 || strings.TrimSpace(req.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method_id and code are required"})
		return
	}

	if err := h.mfaService.ConfirmTOTP(ctx.userRecord, req.MethodID, strings.TrimSpace(req.Code)); err != nil {
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(ctx.userRecord.Email, ctx.userRecord.Name, ctx.ipAddress, ctx.userAgent)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.database != nil {
		label := strings.TrimSpace(req.Label)
		if label == "" {
			label = "Authenticator App"
		}
		if err := h.database.UpdateMFAMethodLabel(req.MethodID, label); err != nil {
			log.Printf("failed updating MFA method label: %v", err)
		}
	}

	trustedDeviceID, trustedCookieSet, ok := h.handleTrustedDevice(c, ctx, req.TrustDevice)
	if !ok {
		return
	}

	now, ok := h.updateSessionAfterMFA(c, ctx, trustedDeviceID, trustedCookieSet)
	if !ok {
		return
	}

	// Audit log for MFA enrollment completion
	if h.database != nil {
		methodLabel := strings.TrimSpace(req.Label)
		if methodLabel == "" {
			methodLabel = "Authenticator App"
		}
		details, _ := json.Marshal(map[string]interface{}{
			"label": methodLabel,
		})
		if err := h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    ctx.userRecord.Email,
			UserName:     ctx.userRecord.Name,
			ActionType:   "auth",
			Action:       audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),
			ResourceType: "mfa_method",
			Resource:     "totp",
			Details:      string(details),
			Status:       "success",
			IPAddress:    ctx.ipAddress,
			UserAgent:    ctx.userAgent,
		}); err != nil {
			log.Printf(
				`{"level":"error","event":"audit_log_failed","action":audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),"error":%q}`,
				err,
			)
		}
	}

	response := VerifyMFAResponse{
		MFAVerifiedAt:         now,
		RecentStepUpExpiresAt: now.Add(h.stepUpWindow()),
		TrustedDeviceCookie:   trustedCookieSet,
	}
	c.JSON(http.StatusOK, response)
}

// StartYubiOTPEnrollment godoc
// @Summary Start Yubico OTP enrollment
// @Description Enrolls a Yubikey using Yubico OTP. Touch your Yubikey to generate an OTP.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body StartYubiOTPEnrollmentRequest true "Yubikey OTP"
// @Success 200 {object} StartYubiOTPEnrollmentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/enroll/yubiotp/start [post]
func (h *AuthHandler) StartYubiOTPEnrollment(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	var req StartYubiOTPEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf(`{"level":"error","event":"yubiotp_bind_failed","error":%q}`, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	result, err := h.mfaService.StartYubiOTPEnrollment(ctx.userRecord, req.YubiOTP)
	if err != nil {
		log.Printf(
			`{"level":"error","event":"yubiotp_enrollment_failed","user":%q,"error":%q}`,
			ctx.authUser.Email,
			err.Error(),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// StartWebAuthnEnrollment godoc
// @Summary Start WebAuthn enrollment
// @Description Begins WebAuthn credential registration. Returns a challenge for the browser.
// @Tags Auth
// @Accept json
// @Produce json
// @Success 200 {object} StartWebAuthnEnrollmentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/enroll/webauthn/start [post]
func (h *AuthHandler) StartWebAuthnEnrollment(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	result, sessionData, err := h.mfaService.StartWebAuthnEnrollment(ctx.userRecord)
	if err != nil {
		log.Printf(
			`{"level":"error","event":"webauthn_start_failed","user":%q,"error":%q}`,
			ctx.authUser.Email,
			err.Error(),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Store WebAuthn session data in the user's HTTP session metadata
	sessionID, ok := auth.GetSessionID(c.Request.Context())
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session not found"})
		return
	}

	// Update session metadata with WebAuthn session
	metadata := map[string]interface{}{
		"webauthn_enrollment_session": sessionData,
		"webauthn_enrollment_expires": time.Now().Add(5 * time.Minute).Unix(),
	}
	if err := h.database.UpdateSessionMetadata(sessionID, metadata); err != nil {
		log.Printf("failed to store webauthn session: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store session"})
		return
	}

	// Debug: log what we're returning
	optionsJSON, _ := json.Marshal(result.PublicKeyOptions)
	log.Printf(
		"WebAuthn enrollment start - MethodID: %d, PublicKeyOptions JSON: %s",
		result.MethodID,
		string(optionsJSON),
	)

	response := StartWebAuthnEnrollmentResponse{
		MethodID:         result.MethodID,
		PublicKeyOptions: result.PublicKeyOptions,
		BackupCodes:      result.BackupCodes,
	}
	c.JSON(http.StatusOK, response)
}

// ConfirmWebAuthnEnrollment godoc
// @Summary Confirm WebAuthn enrollment
// @Description Completes WebAuthn credential registration with the client's credential response.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ConfirmWebAuthnEnrollmentRequest true "WebAuthn credential"
// @Success 200 {object} WebAuthnEnrollmentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/enroll/webauthn/confirm [post]
func (h *AuthHandler) ConfirmWebAuthnEnrollment(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	var req ConfirmWebAuthnEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Retrieve WebAuthn session from HTTP session metadata
	sessionID, ok := auth.GetSessionID(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	session, err := h.database.GetSessionByID(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load session"})
		return
	}

	var sessionMeta map[string]interface{}
	if len(session.Metadata) > 0 {
		if err := json.Unmarshal(session.Metadata, &sessionMeta); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid session metadata"})
			return
		}
	}

	sessionDataStr, ok := sessionMeta["webauthn_enrollment_session"].(string)
	if !ok || sessionDataStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "webauthn session not found or expired"})
		return
	}

	// Check expiration
	expiresFloat, ok := sessionMeta["webauthn_enrollment_expires"].(float64)
	if ok && time.Now().Unix() > int64(expiresFloat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "webauthn session expired"})
		return
	}

	// Marshal credential to JSON
	credentialJSON, err := json.Marshal(req.Credential)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credential format"})
		return
	}

	label := req.Label
	if label == "" {
		label = "Security Key"
	}

	methodID, err := h.mfaService.ConfirmWebAuthnEnrollment(
		ctx.userRecord,
		req.MethodID,
		[]byte(sessionDataStr),
		credentialJSON,
		label,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Clear WebAuthn session from metadata
	delete(sessionMeta, "webauthn_enrollment_session")
	delete(sessionMeta, "webauthn_enrollment_expires")
	if err := h.database.UpdateSessionMetadata(sessionID, sessionMeta); err != nil {
		log.Printf("failed to clear webauthn session: %v", err)
	}

	_, ok = h.updateSessionAfterMFA(c, ctx, ctx.authUser.Session.TrustedDeviceID, false)
	if !ok {
		return
	}

	// Audit log for WebAuthn enrollment completion
	if h.database != nil {
		details, _ := json.Marshal(map[string]interface{}{
			"label": label,
		})
		if err := h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    ctx.userRecord.Email,
			UserName:     ctx.userRecord.Name,
			ActionType:   "auth",
			Action:       audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),
			ResourceType: "mfa_method",
			Resource:     "webauthn",
			Details:      string(details),
			Status:       "success",
			IPAddress:    ctx.ipAddress,
			UserAgent:    ctx.userAgent,
		}); err != nil {
			log.Printf(
				`{"level":"error","event":"audit_log_failed","action":audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),"error":%q}`,
				err,
			)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "enrolled", "method_id": methodID})
}
