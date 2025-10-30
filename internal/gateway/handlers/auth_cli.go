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
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
)

// CLILoginStart godoc
// @Summary Start CLI OAuth login
// @Description Initiates the CLI OAuth flow and returns PKCE parameters.
// @Tags Auth
// @Produce json
// @Success 200 {object} auth.LoginStartResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/cli/start [post]
func (h *AuthHandler) CLILoginStart(c *gin.Context) {
	resp, err := h.oauth.StartLogin()
	if err != nil {
		h.auditLogin(c, "cli", "error")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.database != nil {
		if err := h.database.StoreCLILoginState(resp.State, resp.CodeVerifier); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize login state"})
			return
		}
	}

	h.auditLogin(c, "cli", "success")
	c.JSON(http.StatusOK, resp)
}

// CLILoginCallback godoc
// @Summary Complete CLI OAuth redirect
// @Description Stores the OAuth authorization code for the CLI to finish login.
// @Tags Auth
// @Param code query string true "Authorization code"
// @Param state query string true "State"
// @Success 307 {string} string "Temporary Redirect"
// @Failure 400 {string} string "Missing parameters"
// @Router /auth/cli/callback [get]
func (h *AuthHandler) CLILoginCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))

	if code == "" || state == "" {
		c.String(http.StatusBadRequest, "Missing code or state")
		return
	}

	// Store the auth code in database for CLI polling
	if h.database != nil {
		if err := h.database.UpdateCLILoginCode(state, code); err != nil {
			c.String(http.StatusInternalServerError, "Failed to persist login state")
			return
		}
	}

	redirect := fmt.Sprintf("%s?state=%s", APIRoute("auth/cli/mfa"), url.QueryEscape(state))
	c.Redirect(http.StatusTemporaryRedirect, redirect)
}

func (h *AuthHandler) CLILoginMFAForm(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	challengeRoute := WebRoute("auth/mfa/challenge")
	buildURL := func(params url.Values) string {
		return buildChallengeURL(challengeRoute, params)
	}

	if state == "" {
		params := url.Values{}
		params.Set("error", "missing_state")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}
	if h.database == nil {
		params := url.Values{}
		params.Set("error", "service_unavailable")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}

	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		params := url.Values{}
		params.Set("error", "load_failure")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}
	if record == nil {
		params := url.Values{}
		params.Set("error", "expired")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}

	if record.LoginError.Valid {
		params := url.Values{}
		params.Set("error", strings.TrimSpace(record.LoginError.String))
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}

	if record.MFAVerifiedAt.Valid {
		c.Redirect(http.StatusTemporaryRedirect, WebRoute("cli/auth/success"))
		return
	}

	var loginEmail string
	if record.LoginEmail.Valid {
		loginEmail = strings.TrimSpace(record.LoginEmail.String)
	}

	if loginEmail == "" || !record.LoginToken.Valid || !record.LoginExpiresAt.Valid {
		if !record.Code.Valid || !record.CodeVerifier.Valid {
			params := url.Values{}
			params.Set("error", "session_incomplete")
			c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
			return
		}

		loginResp, exchangeErr := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
		if exchangeErr != nil {
			params := url.Values{}
			params.Set("error", "exchange_failed")
			c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
			return
		}

		if err := h.database.SetCLILoginProfile(state, loginResp.Email, loginResp.Name); err != nil {
			params := url.Values{}
			params.Set("error", "persist_failure")
			c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
			return
		}

		loginEmail = strings.TrimSpace(loginResp.Email)
	}

	if loginEmail == "" {
		errMsg := "Unable to determine account information for CLI login."
		if err := h.database.FailCLILoginState(state, errMsg); err != nil {
			log.Printf("cli login fail (missing email): state=%s err=%v", state, err)
		}
		params := url.Values{}
		params.Set("error", "unauthorized")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}

	userRecord, err := h.database.GetUser(loginEmail)
	if err != nil {
		params := url.Values{}
		params.Set("error", "load_failure")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}
	if userRecord == nil {
		if err := h.database.FailCLILoginState(state, "User not authorized for this gateway."); err != nil {
			log.Printf("cli login fail (unknown user): state=%s err=%v", state, err)
		}
		params := url.Values{}
		params.Set("error", "unauthorized")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}

	if !shouldEnforceMFA(h.mfaSettings, userRecord) {
		if err := h.database.MarkCLILoginVerified(state, nil); err != nil {
			log.Printf("cli login mark verified failed: state=%s err=%v", state, err)
			params := url.Values{}
			params.Set("error", "persist_failure")
			c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, WebRoute("cli/auth/success"))
		return
	}

	// Create session for MFA verification (same as web login flow)
	if _, err := h.createLoginSession(c, userRecord, "cli-mfa"); err != nil {
		log.Printf("cli mfa session create failed: user=%s err=%v", userRecord.Email, err)
		params := url.Values{}
		params.Set("error", "session_failed")
		c.Redirect(http.StatusTemporaryRedirect, buildURL(params))
		return
	}

	if !userRecord.MFAEnrolled {
		if err := h.database.FailCLILoginState(state, cliEnrollmentErrorMessage); err != nil {
			log.Printf("cli login fail (enrollment required): state=%s err=%v", state, err)
		}

		// Create web session for enrollment flow
		h.cliHandleEnrollmentRedirect(c, state, userRecord.Email)

		enrollParams := url.Values{}
		enrollParams.Set("enrollment", "required")
		enrollParams.Set("channel", "cli")
		enrollParams.Set("state", state)
		c.Redirect(
			http.StatusTemporaryRedirect,
			fmt.Sprintf("%s?%s", WebRoute("account/security"), enrollParams.Encode()),
		)
		return
	}

	params := url.Values{}
	params.Set("state", state)
	challengeURL := buildURL(params)
	log.Printf(
		"CLI callback: redirecting to MFA challenge: url=%s user=%s state=%s",
		challengeURL,
		userRecord.Email,
		state,
	)
	c.Redirect(http.StatusTemporaryRedirect, challengeURL)
}

// CLILoginMFASubmit handles both TOTP and WebAuthn verification for CLI login
func (h *AuthHandler) CLILoginMFASubmit(c *gin.Context) {
	if h.database == nil || h.mfaService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service_unavailable"})
		return
	}

	var req struct {
		State             string `json:"state"`
		Method            string `json:"method"`             // "totp" or "webauthn" (optional, defaults to totp)
		Code              string `json:"code"`               // For TOTP
		SessionData       string `json:"session_data"`       // For WebAuthn
		AssertionResponse string `json:"assertion_response"` // For WebAuthn
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = "totp" // Default for backwards compatibility
	}

	state := strings.TrimSpace(req.State)
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state_required"})
		return
	}

	if method == "totp" && strings.TrimSpace(req.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code_required"})
		return
	}

	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load_failure"})
		return
	}
	if record == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_expired"})
		return
	}
	if record.MFAVerifiedAt.Valid {
		c.JSON(http.StatusOK, gin.H{"redirect": WebRoute("cli/auth/success")})
		return
	}

	if !record.LoginEmail.Valid {
		if !record.Code.Valid || !record.CodeVerifier.Valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_incomplete"})
			return
		}
		loginResp, exchangeErr := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
		if exchangeErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "exchange_failed"})
			return
		}
		if err := h.database.SetCLILoginProfile(state, loginResp.Email, loginResp.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "persist_failure"})
			return
		}
		record.LoginEmail.String = loginResp.Email
		record.LoginEmail.Valid = true
	}

	userRecord, err := h.database.GetUser(strings.TrimSpace(record.LoginEmail.String))
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Verify with appropriate method
	var verification *mfa.VerificationResult
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	switch method {
	case "totp":
		verification, err = h.mfaService.VerifyTOTP(userRecord, strings.TrimSpace(req.Code), ipAddress, userAgent, nil)
	case "webauthn":
		verification, err = h.mfaService.VerifyWebAuthnAssertion(
			userRecord,
			[]byte(req.SessionData),
			[]byte(req.AssertionResponse),
			ipAddress,
			userAgent,
			nil,
		)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_method"})
		return
	}

	if err != nil {
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(
				userRecord.Email,
				userRecord.Name,
				c.ClientIP(),
				c.GetHeader("User-Agent"),
			)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_code"})
		return
	}

	var methodID *int64
	if verification != nil && verification.MethodID > 0 {
		methodID = &verification.MethodID
	}

	if err := h.database.MarkCLILoginVerified(state, methodID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist_failure"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"redirect": WebRoute("cli/auth/success")})
}

// CLILoginComplete godoc
// @Summary Finalize CLI OAuth login
// @Description Exchanges the stored authorization code and PKCE verifier for a session token.
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
	record, err := h.database.GetCLILoginState(req.State)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load login state"})
		return
	}
	if record == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired state"})
		return
	}
	if record.LoginError.Valid {
		reason := strings.TrimSpace(record.LoginError.String)
		if reason == "" {
			reason = "login_failed"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": reason})
		return
	}
	if !record.LoginEmail.Valid || !record.MFAVerifiedAt.Valid {
		c.JSON(http.StatusAccepted, gin.H{"status": "pending"})
		return
	}

	userRecord, err := h.database.GetUser(record.LoginEmail.String)
	if err != nil || userRecord == nil {
		// Notify about unauthorized CLI login attempt
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return
	}

	if h.sessions == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session manager not available"})
		return
	}

	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		deviceID = uuid.NewString()
	}

	deviceName := strings.TrimSpace(req.DeviceName)
	deviceMeta := map[string]interface{}{}
	if trimmed := strings.TrimSpace(req.DeviceOS); trimmed != "" {
		deviceMeta["os"] = trimmed
	}
	if trimmed := strings.TrimSpace(req.ClientVersion); trimmed != "" {
		deviceMeta["client_version"] = trimmed
	}

	extra := map[string]interface{}{"login_flow": "cli"}
	sessionTTL := 90 * 24 * time.Hour
	sessionToken, session, err := h.sessions.CreateSession(userRecord, auth.SessionMetadata{
		Channel:        "cli",
		DeviceID:       deviceID,
		DeviceName:     deviceName,
		DeviceMetadata: deviceMeta,
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Extra:          extra,
		TTLOverride:    sessionTTL,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	if record.MFAVerifiedAt.Valid {
		if mfaTime, ok := h.cliStampMFAVerification(session.ID, record.MFAVerifiedAt.Time); ok {
			session.MFAVerifiedAt = &mfaTime
			session.RecentStepUpAt = &mfaTime
		}
	}

	enforceMFA := shouldEnforceMFA(h.mfaSettings, userRecord)
	mfaRequired := h.isMFARequired(userRecord) && session.MFAVerifiedAt == nil
	enrollmentRequired := enforceMFA && !userRecord.MFAEnrolled
	name := userRecord.Name
	if record.LoginName.Valid && strings.TrimSpace(record.LoginName.String) != "" {
		name = record.LoginName.String
	}

	response := CLILoginResponse{
		Token:              sessionToken,
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

	_ = h.database.DeleteCLILoginState(req.State)

	// Notify about successful CLI login
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

	c.JSON(http.StatusOK, response)
}
