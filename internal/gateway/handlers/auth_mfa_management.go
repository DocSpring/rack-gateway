package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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
	codes, err := h.mfaService.GenerateBackupCodes(userRecord.ID)
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
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}
	methods, err := h.database.ListMFAMethods(userRecord.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list mfa methods"})
		return
	}
	trustedDevices, err := h.database.ListTrustedDevices(userRecord.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list trusted devices"})
		return
	}
	backupCodes, err := h.database.ListBackupCodes(userRecord.ID)
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
	if authUser.Session != nil && authUser.Session.RecentStepUpAt != nil {
		expires := authUser.Session.RecentStepUpAt.Add(h.stepUpWindow())
		recentExpires = &expires
	}
	response := MFAStatusResponse{
		Enrolled:              userRecord.MFAEnrolled,
		Required:              shouldEnforceMFA(h.mfaSettings, userRecord),
		Methods:               methodResp,
		TrustedDevices:        trustedResp,
		BackupCodes:           summary,
		RecentStepUpExpiresAt: recentExpires,
		PreferredMethod:       userRecord.PreferredMFAMethod,
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
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}
	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}
	methodIDParam := strings.TrimSpace(c.Param("methodID"))
	if methodIDParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method id required"})
		return
	}
	methodID, err := strconv.ParseInt(methodIDParam, 10, 64)
	if err != nil || methodID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid method id"})
		return
	}
	method, err := h.database.GetMFAMethodByID(methodID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load mfa method"})
		return
	}
	if method == nil || method.UserID != userRecord.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "mfa method not found"})
		return
	}
	if err := h.database.DeleteMFAMethod(method.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete mfa method"})
		return
	}
	remaining, err := h.database.ListMFAMethods(userRecord.ID)
	if err == nil {
		hasConfirmed := false
		for _, candidate := range remaining {
			if candidate != nil && candidate.ConfirmedAt != nil {
				hasConfirmed = true
				break
			}
		}
		if !hasConfirmed {
			if err := h.database.SetUserMFAEnrolled(userRecord.ID, false); err != nil {
				log.Printf("failed to update mfa enrollment after delete: %v", err)
			}
			// Clear all trusted devices when MFA is fully disabled
			trustedDevices, err := h.database.ListTrustedDevices(userRecord.ID)
			if err != nil {
				log.Printf("failed to list trusted devices: %v", err)
			} else {
				for _, device := range trustedDevices {
					if device != nil && device.RevokedAt == nil {
						if err := h.database.RevokeTrustedDevice(device.ID, "mfa_disabled"); err != nil {
							log.Printf("failed to revoke trusted device %d: %v", device.ID, err)
						}
					}
				}
			}
		}
	} else {
		log.Printf("failed to list remaining mfa methods: %v", err)
	}
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
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}
	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}
	methodIDParam := strings.TrimSpace(c.Param("methodID"))
	if methodIDParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method id required"})
		return
	}
	methodID, err := strconv.ParseInt(methodIDParam, 10, 64)
	if err != nil || methodID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid method id"})
		return
	}
	method, err := h.database.GetMFAMethodByID(methodID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load mfa method"})
		return
	}
	if method == nil || method.UserID != userRecord.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "mfa method not found"})
		return
	}

	var req UpdateMFAMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Update only the label
	if err := h.database.UpdateMFAMethodLabel(method.ID, req.Label); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update mfa method"})
		return
	}

	// Audit log
	if h.database != nil {
		details, _ := json.Marshal(map[string]interface{}{
			"method_id": method.ID,
			"label":     req.Label,
		})
		if err := h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userRecord.Email,
			UserName:     userRecord.Name,
			ActionType:   "auth",
			Action:       audit.BuildAction(rbac.ResourceStringMFA, rbac.ActionStringUpdate),
			ResourceType: "mfa_method",
			Resource:     fmt.Sprintf("%d", method.ID),
			Details:      string(details),
			Status:       "success",
			IPAddress:    c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
		}); err != nil {
			log.Printf("failed to log mfa update audit: %v", err)
		}
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
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
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return
	}
	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}
	deviceIDParam := strings.TrimSpace(c.Param("deviceID"))
	if deviceIDParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device id required"})
		return
	}
	deviceID, err := strconv.ParseInt(deviceIDParam, 10, 64)
	if err != nil || deviceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	device, err := h.database.GetTrustedDeviceByID(deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load trusted device"})
		return
	}
	if device == nil || device.UserID != userRecord.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "trusted device not found"})
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
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "user session required"})
		return
	}

	var req UpdatePreferredMFAMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Validate method if provided
	if req.PreferredMethod != nil {
		method := *req.PreferredMethod
		if method != "totp" && method != "webauthn" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "preferred_method must be 'totp' or 'webauthn'"})
			return
		}
	}

	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	if err := h.database.UpdatePreferredMFAMethod(userRecord.ID, req.PreferredMethod); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update preferred method"})
		return
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}
