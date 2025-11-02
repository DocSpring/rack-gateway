package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func cliRedirectWithError(c *gin.Context, errorCode string) {
	challengeRoute := WebRoute("auth/mfa/challenge")
	params := url.Values{}
	params.Set("error", errorCode)
	c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(challengeRoute, params))
}

func buildChallengeURL(base string, params url.Values) string {
	return fmt.Sprintf("%s?%s", base, params.Encode())
}

func (h *AuthHandler) cliLoadAndValidateUser(c *gin.Context, state, loginEmail string) (*db.User, bool) {
	userRecord, err := h.database.GetUser(loginEmail)
	if err != nil {
		cliRedirectWithError(c, "load_failure")
		return nil, false
	}
	if userRecord == nil {
		if err := h.database.FailCLILoginState(state, "User not authorized for this gateway."); err != nil {
			log.Printf("cli login fail (unknown user): state=%s err=%v", state, err)
		}
		cliRedirectWithError(c, "unauthorized")
		return nil, false
	}
	return userRecord, true
}

func (h *AuthHandler) cliHandleNoMFARequired(c *gin.Context, state string) {
	if err := h.database.MarkCLILoginVerified(state, nil); err != nil {
		log.Printf("cli login mark verified failed: state=%s err=%v", state, err)
		cliRedirectWithError(c, "persist_failure")
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, WebRoute("cli/auth/success"))
}

func (h *AuthHandler) cliHandleEnrollmentRequired(c *gin.Context, state, _ string) {
	if err := h.database.FailCLILoginState(state, cliEnrollmentErrorMessage); err != nil {
		log.Printf("cli login fail (enrollment required): state=%s err=%v", state, err)
	}

	enrollParams := url.Values{}
	enrollParams.Set("enrollment", "required")
	enrollParams.Set("channel", "cli")
	enrollParams.Set("state", state)
	c.Redirect(
		http.StatusTemporaryRedirect,
		fmt.Sprintf("%s?%s", WebRoute("account/security"), enrollParams.Encode()),
	)
}

func (h *AuthHandler) cliExchangeOAuthCode(
	c *gin.Context,
	record *db.CLILoginState,
	state string,
	useJSON bool,
) (string, bool) {
	if !record.Code.Valid || !record.CodeVerifier.Valid {
		if useJSON {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_incomplete"})
		} else {
			cliRedirectWithError(c, "session_incomplete")
		}
		return "", false
	}

	loginResp, err := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
	if err != nil {
		if useJSON {
			c.JSON(http.StatusBadRequest, gin.H{"error": "exchange_failed"})
		} else {
			cliRedirectWithError(c, "exchange_failed")
		}
		return "", false
	}

	if err := h.database.SetCLILoginProfile(state, loginResp.Email, loginResp.Name); err != nil {
		if useJSON {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "persist_failure"})
		} else {
			cliRedirectWithError(c, "persist_failure")
		}
		return "", false
	}

	return strings.TrimSpace(loginResp.Email), true
}

func (h *AuthHandler) cliExchangeIfNeeded(
	c *gin.Context,
	record *db.CLILoginState,
	state string,
	useJSON bool,
) (*db.CLILoginState, bool) {
	// If login email is already set, OAuth exchange has completed
	// LoginToken and LoginExpiresAt are only set for non-MFA flows
	if record.LoginEmail.Valid {
		return record, true
	}

	_, ok := h.cliExchangeOAuthCode(c, record, state, useJSON)
	if !ok {
		return nil, false
	}

	// Reload record after exchange
	refreshed, err := h.database.GetCLILoginState(state)
	if err != nil || refreshed == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload_failure"})
		return nil, false
	}

	return refreshed, true
}

func (h *AuthHandler) cliGetUserRecord(c *gin.Context, loginEmail string) (*db.User, bool) {
	userRecord, err := h.database.GetUser(loginEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load_failure"})
		return nil, false
	}
	if userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_not_authorized"})
		return nil, false
	}
	return userRecord, true
}

func (h *AuthHandler) cliStampMFAVerification(sessionID int64, verifiedAt time.Time) (time.Time, bool) {
	if h.sessions == nil {
		return time.Time{}, false
	}

	if err := h.sessions.UpdateSessionMFAVerified(sessionID, verifiedAt, nil); err != nil {
		log.Printf("failed to update session mfa timestamp: %v", err)
		return time.Time{}, false
	}

	return verifiedAt, true
}

func (h *AuthHandler) bindCLILoginComplete(c *gin.Context) (CLILoginCompleteRequest, bool) {
	var req CLILoginCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return CLILoginCompleteRequest{}, false
	}
	return req, true
}

func (h *AuthHandler) loadCLILoginState(c *gin.Context, state string) (*db.CLILoginState, bool) {
	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load login state"})
		return nil, false
	}
	if record == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired state"})
		return nil, false
	}
	return record, true
}

func (h *AuthHandler) handleCLIRecordErrors(c *gin.Context, record *db.CLILoginState) bool {
	if record.LoginError.Valid {
		reason := strings.TrimSpace(record.LoginError.String)
		if reason == "" {
			reason = "login_failed"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": reason})
		return true
	}
	return false
}

func (h *AuthHandler) respondIfPending(c *gin.Context, record *db.CLILoginState) bool {
	if !record.LoginEmail.Valid || !record.MFAVerifiedAt.Valid {
		c.JSON(http.StatusAccepted, gin.H{"status": "pending"})
		return true
	}
	return false
}

func (h *AuthHandler) loadUserForCLI(c *gin.Context, record *db.CLILoginState) (*db.User, bool) {
	userRecord, err := h.database.GetUser(record.LoginEmail.String)
	if err != nil || userRecord == nil {
		h.notifyUnauthorizedCLILogin(c, record)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return nil, false
	}
	if h.sessions == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session manager not available"})
		return nil, false
	}
	return userRecord, true
}

func (h *AuthHandler) createCLISessionOrRespond(
	c *gin.Context,
	userRecord *db.User,
	req CLILoginCompleteRequest,
) (string, *db.UserSession, bool) {
	sessionToken, session, err := h.createCLISession(c, userRecord, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return "", nil, false
	}
	return sessionToken, session, true
}

func (h *AuthHandler) applyMFATimestamps(record *db.CLILoginState, session *db.UserSession) {
	if record.MFAVerifiedAt.Valid {
		if mfaTime, ok := h.cliStampMFAVerification(session.ID, record.MFAVerifiedAt.Time); ok {
			session.MFAVerifiedAt = &mfaTime
			session.RecentStepUpAt = &mfaTime
		}
	}
}

func (h *AuthHandler) notifyCLILoginComplete(c *gin.Context, userRecord *db.User) {
	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(
			userRecord.Email,
			userRecord.Name,
			"cli",
			"complete",
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			true,
		)
	}
}

func (h *AuthHandler) notifyUnauthorizedCLILogin(c *gin.Context, record *db.CLILoginState) {
	userName := ""
	if record.LoginName.Valid {
		userName = record.LoginName.String
	}
	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(
			record.LoginEmail.String,
			userName,
			"cli",
			"user_not_authorized",
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			false,
		)
	}
}

func (h *AuthHandler) createCLISession(
	c *gin.Context,
	user *db.User,
	req CLILoginCompleteRequest,
) (string, *db.UserSession, error) {
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		deviceID = uuid.NewString()
	}

	deviceMeta := map[string]interface{}{}
	if trimmed := strings.TrimSpace(req.DeviceOS); trimmed != "" {
		deviceMeta["os"] = trimmed
	}
	if trimmed := strings.TrimSpace(req.ClientVersion); trimmed != "" {
		deviceMeta["client_version"] = trimmed
	}

	return h.sessions.CreateSession(user, auth.SessionMetadata{
		Channel:        "cli",
		DeviceID:       deviceID,
		DeviceName:     strings.TrimSpace(req.DeviceName),
		DeviceMetadata: deviceMeta,
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Extra:          map[string]interface{}{"login_flow": "cli"},
		TTLOverride:    90 * 24 * time.Hour,
	})
}

func (h *AuthHandler) buildCLILoginResponse(
	user *db.User,
	record *db.CLILoginState,
	token string,
	session *db.UserSession,
) CLILoginResponse {
	enforceMFA := shouldEnforceMFA(h.mfaSettings, user)
	mfaRequired := h.isMFARequired(user) && session.MFAVerifiedAt == nil
	enrollmentRequired := enforceMFA && !user.MFAEnrolled

	name := user.Name
	if record.LoginName.Valid && strings.TrimSpace(record.LoginName.String) != "" {
		name = record.LoginName.String
	}

	return CLILoginResponse{
		Token:              token,
		Email:              record.LoginEmail.String,
		Name:               name,
		ExpiresAt:          session.ExpiresAt,
		SessionID:          session.ID,
		Channel:            session.Channel,
		DeviceID:           session.DeviceID,
		DeviceName:         session.DeviceName,
		MFAVerified:        session.MFAVerifiedAt != nil,
		MFARequired:        mfaRequired,
		EnrollmentRequired: enrollmentRequired,
	}
}
