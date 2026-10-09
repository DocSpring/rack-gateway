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

const cliSessionTTL = 90 * 24 * time.Hour

func cliRedirectWithError(c *gin.Context, errorCode string) {
	params := url.Values{}
	params.Set("error", errorCode)
	c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(WebRoute("auth/mfa/challenge"), params))
}

func buildChallengeURL(base string, params url.Values) string {
	return fmt.Sprintf("%s?%s", base, params.Encode())
}

// CLILoginComplete godoc
// @Summary Finalize CLI login
// @Description Redeems the single-use login code delivered to the CLI's loopback listener, proving
// @Description possession of the PKCE code verifier, and returns a CLI session token.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body CLILoginCompleteRequest true "CLI login payload"
// @Success 200 {object} CLILoginResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/cli/complete [post]
func (h *AuthHandler) CLILoginComplete(c *gin.Context) {
	var req CLILoginCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if h.database == nil || h.sessions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service_unavailable"})
		return
	}

	// Consuming deletes the login, so each login code can be tried exactly once.
	record, err := h.database.ConsumeCLILoginCode(hashCLISecret(strings.TrimSpace(req.LoginCode)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load login"})
		return
	}
	if record == nil || !record.MFAVerifiedAt.Valid || !record.LoginEmail.Valid ||
		!pkceS256Matches(strings.TrimSpace(req.CodeVerifier), record.CLICodeChallenge) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired login code"})
		return
	}

	userRecord, err := h.database.GetUser(record.LoginEmail.String)
	if err != nil || userRecord == nil {
		h.notifyUnauthorizedCLILogin(c, record)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}

	sessionToken, session, err := h.createCLISession(c, userRecord, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}
	h.cliStampMFAVerification(session, record.MFAVerifiedAt.Time)

	h.notifyCLILoginComplete(c, userRecord, session)
	c.JSON(http.StatusOK, h.buildCLILoginResponse(userRecord, record, sessionToken, session))
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

// cliStampMFAVerification carries the browser-side MFA verification over to the new CLI session.
func (h *AuthHandler) cliStampMFAVerification(session *db.UserSession, verifiedAt time.Time) {
	if err := h.sessions.UpdateSessionMFAVerified(session.ID, verifiedAt, nil); err != nil {
		log.Printf("failed to update session mfa timestamp: %v", err)
		return
	}
	session.MFAVerifiedAt = &verifiedAt
	session.RecentStepUpAt = &verifiedAt
}

func (h *AuthHandler) notifyCLILoginComplete(c *gin.Context, userRecord *db.User, session *db.UserSession) {
	if h.securityNotifier == nil {
		return
	}
	h.securityNotifier.LoginAttempt(
		userRecord.Email,
		userRecord.Name,
		"cli",
		"complete",
		c.ClientIP(),
		c.GetHeader("User-Agent"),
		true,
	)
	h.securityNotifier.NewCLISession(
		userRecord.Email,
		userRecord.Name,
		session.DeviceName,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
	)
}

func (h *AuthHandler) notifyUnauthorizedCLILogin(c *gin.Context, record *db.CLILoginState) {
	if h.securityNotifier == nil {
		return
	}
	userName := ""
	if record.LoginName.Valid {
		userName = record.LoginName.String
	}
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
		DeviceName:     sanitizeDeviceName(req.DeviceName),
		DeviceMetadata: deviceMeta,
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Extra:          map[string]interface{}{"login_flow": "cli"},
		TTLOverride:    cliSessionTTL,
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
