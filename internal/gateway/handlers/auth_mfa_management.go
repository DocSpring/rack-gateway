package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/gin-gonic/gin"
)

// RegenerateBackupCodes godoc
// @Summary Regenerate backup codes
// @Description Generates a new set of MFA backup codes. All prior codes become invalid.
// @Tags Auth
// @Produce json
// @Success 200 {object} BackupCodesResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/backup-codes/regenerate [post]
func (h *AuthHandler) RegenerateBackupCodes(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	codes, err := h.mfaService.GenerateBackupCodes(ctx.userRecord.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, BackupCodesResponse{BackupCodes: codes})
}

// GetMFAStatus godoc
// @Summary Get MFA status for current session
// @Description Returns enrollment state, configured methods, trusted devices, and backup code summary.
// @Tags Auth
// @Produce json
// @Success 200 {object} MFAStatusResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /auth/mfa/status [get]
func (h *AuthHandler) GetMFAStatus(c *gin.Context) {
	ctx, ok := h.getMFAContext(c)
	if !ok {
		return
	}

	methods, err := h.database.ListMFAMethods(ctx.userRecord.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list mfa methods"})
		return
	}
	trustedDevices, err := h.database.ListTrustedDevices(ctx.userRecord.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list trusted devices"})
		return
	}
	backupCodes, err := h.database.ListBackupCodes(ctx.userRecord.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list backup codes"})
		return
	}
	methodResp := make([]MFAMethodResponse, 0, len(methods))
	for _, method := range methods {
		if method == nil {
			continue
		}
		methodResp = append(methodResp, makeMFAMethodResponse(method))
	}
	trustedResp := make([]TrustedDeviceResponse, 0, len(trustedDevices))
	for _, device := range trustedDevices {
		if device == nil {
			continue
		}
		if device.RevokedAt != nil {
			continue
		}
		trustedResp = append(trustedResp, makeTrustedDeviceResponse(device))
	}
	summary := summarizeBackupCodes(backupCodes)
	var recentExpires *time.Time
	if ctx.authUser.Session != nil && ctx.authUser.Session.RecentStepUpAt != nil {
		expires := ctx.authUser.Session.RecentStepUpAt.Add(h.stepUpWindow())
		recentExpires = &expires
	}
	response := MFAStatusResponse{
		Enrolled:              ctx.userRecord.MFAEnrolled,
		Required:              shouldEnforceMFA(h.mfaSettings, ctx.userRecord),
		Methods:               methodResp,
		TrustedDevices:        trustedResp,
		BackupCodes:           summary,
		RecentStepUpExpiresAt: recentExpires,
		PreferredMethod:       ctx.userRecord.PreferredMFAMethod,
		WebAuthnAvailable:     h.mfaService.IsWebAuthnConfigured(),
	}
	c.JSON(http.StatusOK, response)
}

// DeleteMFAMethod godoc
// @Summary Delete an MFA method
// @Description Removes an existing MFA method for the current user.
// @Tags Auth
// @Param methodID path int true "MFA method ID"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/methods/{methodID} [delete]
func (h *AuthHandler) DeleteMFAMethod(c *gin.Context) {
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return
	}

	userCtx, ok := loadMFAUserContext(c, h.database)
	if !ok {
		return
	}

	methodID, ok := parseIDParam(c, "methodID")
	if !ok {
		return
	}

	method, ok := loadMFAMethod(c, h.database, methodID, userCtx.userRecord.ID)
	if !ok {
		return
	}
	if err := h.database.DeleteMFAMethod(method.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete mfa method"})
		return
	}

	h.handleMFADisablement(userCtx.userRecord.ID)
	c.JSON(http.StatusOK, StatusResponse{Status: "deleted"})
}

// UpdateMFAMethod godoc
// @Summary Update MFA method label
// @Description Updates the label of an MFA method
// @Tags Auth
// @Accept json
// @Produce json
// @Param methodID path int true "MFA Method ID"
// @Param request body UpdateMFAMethodRequest true "Update request"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/mfa/methods/{methodID} [put]
func (h *AuthHandler) UpdateMFAMethod(c *gin.Context) {
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return
	}

	userCtx, ok := loadMFAUserContext(c, h.database)
	if !ok {
		return
	}

	methodID, ok := parseIDParam(c, "methodID")
	if !ok {
		return
	}

	method, ok := loadMFAMethod(c, h.database, methodID, userCtx.userRecord.ID)
	if !ok {
		return
	}

	var req UpdateMFAMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := h.database.UpdateMFAMethodLabel(method.ID, req.Label); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update mfa method"})
		return
	}

	h.auditMFAUpdate(c, userCtx.userRecord, method.ID, req.Label)
	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// TrustCurrentDevice godoc
// @Summary Trust the current device
// @Description Marks the current browser session as trusted by minting a trusted device cookie.
// @Tags Auth
// @Produce json
// @Success 200 {object} VerifyMFAResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/trusted-devices/trust [post]
func (h *AuthHandler) TrustCurrentDevice(c *gin.Context) {
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
		return
	}

	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}

	if authUser.Session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session missing"})
		return
	}

	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}

	payload, err := h.mfaService.MintTrustedDevice(
		userRecord.ID,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to trust device"})
		return
	}

	h.setTrustedDeviceCookie(c, payload.Token)
	now := time.Now()
	trustedDeviceID := &payload.RecordID

	if err := h.sessions.UpdateSessionMFAVerified(authUser.Session.ID, now, trustedDeviceID); err != nil {
		log.Printf("failed updating session after trusting device: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update session"})
		return
	}
	authUser.Session.MFAVerifiedAt = &now
	authUser.Session.TrustedDeviceID = trustedDeviceID

	if err := h.sessions.UpdateSessionRecentStepUp(authUser.Session.ID, now); err != nil {
		log.Printf("failed updating step-up timestamp after trusting device: %v", err)
	} else {
		authUser.Session.RecentStepUpAt = &now
	}

	if err := h.sessions.AttachTrustedDeviceToSession(authUser.Session.ID, *trustedDeviceID); err != nil {
		log.Printf("failed attaching trusted device to session: %v", err)
	}

	response := VerifyMFAResponse{
		MFAVerifiedAt:         now,
		RecentStepUpExpiresAt: now.Add(h.stepUpWindow()),
		TrustedDeviceCookie:   true,
	}

	c.JSON(http.StatusOK, response)
}

// RevokeTrustedDevice godoc
// @Summary Revoke a trusted device
// @Description Revokes a trusted device, requiring MFA on next login from that device.
// @Tags Auth
// @Param deviceID path int true "Trusted device ID"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/trusted-devices/{deviceID} [delete]
func (h *AuthHandler) RevokeTrustedDevice(c *gin.Context) {
	userCtx, ok := loadMFAUserContext(c, h.database)
	if !ok {
		return
	}

	deviceID, ok := parseIDParam(c, "deviceID")
	if !ok {
		return
	}

	device, ok := loadTrustedDevice(c, h.database, deviceID, userCtx.userRecord.ID)
	if !ok {
		return
	}

	if device.RevokedAt != nil {
		c.JSON(http.StatusOK, StatusResponse{Status: "revoked"})
		return
	}

	if err := h.database.RevokeTrustedDevice(device.ID, "user_request"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke trusted device"})
		return
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "revoked"})
}

// UpdatePreferredMFAMethod godoc
// @Summary Update preferred MFA method
// @Description Sets the user's preferred MFA method for sign-in (totp or webauthn)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body UpdatePreferredMFAMethodRequest true "Preferred method"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/preferred-method [put]
func (h *AuthHandler) UpdatePreferredMFAMethod(c *gin.Context) {
	userCtx, ok := loadMFAUserContext(c, h.database)
	if !ok {
		return
	}

	var req UpdatePreferredMFAMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.PreferredMethod != nil {
		method := *req.PreferredMethod
		if method != "totp" && method != "webauthn" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "preferred_method must be 'totp' or 'webauthn'"})
			return
		}
	}

	if err := h.database.UpdatePreferredMFAMethod(userCtx.userRecord.ID, req.PreferredMethod); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update preferred method"})
		return
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}
