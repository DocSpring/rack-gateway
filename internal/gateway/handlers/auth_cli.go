package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

type cliMFASubmit struct {
	state             string
	method            string
	code              string
	sessionData       string
	assertionResponse string
}

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

// CLILoginMFAForm godoc
// @Summary Display MFA challenge form
// @Description Displays the MFA challenge form for CLI login.
// @Tags Auth
// @Param state query string true "State"
// @Success 307 {string} string "Temporary Redirect"
// @Failure 400 {string} string "Missing parameters"
// @Router /auth/cli/mfa [get]
func (h *AuthHandler) CLILoginMFAForm(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		cliRedirectWithError(c, "missing_state")
		return
	}
	if h.database == nil {
		cliRedirectWithError(c, "service_unavailable")
		return
	}

	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		cliRedirectWithError(c, "load_failure")
		return
	}
	if record == nil {
		cliRedirectWithError(c, "expired")
		return
	}

	if record.LoginError.Valid {
		challengeRoute := WebRoute("auth/mfa/challenge")
		params := url.Values{}
		params.Set("error", strings.TrimSpace(record.LoginError.String))
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(challengeRoute, params))
		return
	}

	if record.MFAVerifiedAt.Valid {
		c.Redirect(http.StatusTemporaryRedirect, WebRoute("cli/auth/success"))
		return
	}

	loginEmail := h.resolveCLILoginEmail(c, record, state)
	if loginEmail == "" {
		return
	}

	userRecord, ok := h.cliLoadAndValidateUser(c, state, loginEmail)
	if !ok {
		return
	}

	if !shouldEnforceMFA(h.mfaSettings, userRecord) {
		h.cliHandleNoMFARequired(c, state)
		return
	}

	if _, err := h.createLoginSession(c, userRecord, "cli-mfa"); err != nil {
		log.Printf("cli mfa session create failed: user=%s err=%v", userRecord.Email, err)
		cliRedirectWithError(c, "session_failed")
		return
	}

	if !userRecord.MFAEnrolled {
		h.cliHandleEnrollmentRequired(c, state, userRecord.Email)
		return
	}

	challengeRoute := WebRoute("auth/mfa/challenge")
	params := url.Values{}
	params.Set("state", state)
	challengeURL := buildChallengeURL(challengeRoute, params)
	log.Printf(
		"CLI callback: redirecting to MFA challenge: url=%s user=%s state=%s",
		challengeURL,
		userRecord.Email,
		state,
	)
	c.Redirect(http.StatusTemporaryRedirect, challengeURL)
}

func (h *AuthHandler) resolveCLILoginEmail(
	c *gin.Context,
	record *db.CLILoginState,
	state string,
) string {
	var loginEmail string
	if record.LoginEmail.Valid {
		loginEmail = strings.TrimSpace(record.LoginEmail.String)
	}

	if loginEmail != "" && record.LoginToken.Valid && record.LoginExpiresAt.Valid {
		return loginEmail
	}

	email, _ := h.cliExchangeOAuthCode(c, record, state)
	return email
}

// CLILoginMFASubmit handles both TOTP and WebAuthn verification for CLI login
func (h *AuthHandler) CLILoginMFASubmit(c *gin.Context) {
	if h.database == nil || h.mfaService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service_unavailable"})
		return
	}

	parsed, ok := h.parseMFASubmitRequest(c)
	if !ok {
		return
	}

	record, ok := h.loadMFALoginState(c, parsed.state)
	if !ok {
		return
	}
	if h.shortCircuitIfAlreadyVerified(c, record) {
		return
	}

	record, ok = h.cliExchangeIfNeeded(c, record, parsed.state)
	if !ok {
		return
	}

	userRecord, ok := h.cliGetUserRecord(c, record.LoginEmail.String)
	if !ok {
		return
	}

	verification, err := h.performMFAVerification(
		c,
		userRecord,
		parsed.method,
		parsed.code,
		parsed.sessionData,
		parsed.assertionResponse,
	)
	if err != nil {
		return
	}

	if !h.markCLILoginVerified(c, parsed.state, verification) {
		return
	}

	c.JSON(http.StatusOK, gin.H{"redirect": WebRoute("cli/auth/success")})
}

// parseMFASubmitRequest parses and validates the MFA submit request payload.
func (h *AuthHandler) parseMFASubmitRequest(c *gin.Context) (cliMFASubmit, bool) {
	var req struct {
		State             string `json:"state"`
		Method            string `json:"method"`
		Code              string `json:"code"`
		SessionData       string `json:"session_data"`
		AssertionResponse string `json:"assertion_response"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return cliMFASubmit{}, false
	}
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = "totp"
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state_required"})
		return cliMFASubmit{}, false
	}
	if method == "totp" && strings.TrimSpace(req.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code_required"})
		return cliMFASubmit{}, false
	}
	return cliMFASubmit{
		state:             state,
		method:            method,
		code:              req.Code,
		sessionData:       req.SessionData,
		assertionResponse: req.AssertionResponse,
	}, true
}

// loadMFALoginState loads the CLI login state or writes an error response.
func (h *AuthHandler) loadMFALoginState(c *gin.Context, state string) (*db.CLILoginState, bool) {
	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load_failure"})
		return nil, false
	}
	if record == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_expired"})
		return nil, false
	}
	return record, true
}

// shortCircuitIfAlreadyVerified responds immediately if MFA is already verified.
func (h *AuthHandler) shortCircuitIfAlreadyVerified(c *gin.Context, record *db.CLILoginState) bool {
	if record.MFAVerifiedAt.Valid {
		c.JSON(http.StatusOK, gin.H{"redirect": WebRoute("cli/auth/success")})
		return true
	}
	return false
}

// markCLILoginVerified persists MFA verification and handles errors.
func (h *AuthHandler) markCLILoginVerified(c *gin.Context, state string, verification *mfa.VerificationResult) bool {
	var methodID *int64
	if verification != nil && verification.MethodID > 0 {
		methodID = &verification.MethodID
	}
	if err := h.database.MarkCLILoginVerified(state, methodID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist_failure"})
		return false
	}
	return true
}

func (h *AuthHandler) performMFAVerification(
	c *gin.Context,
	user *db.User,
	method string,
	code string,
	sessionData string,
	assertionResponse string,
) (*mfa.VerificationResult, error) {
	var verification *mfa.VerificationResult
	var err error
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")

	switch method {
	case "totp":
		verification, err = h.mfaService.VerifyTOTP(user, strings.TrimSpace(code), ipAddress, userAgent, nil)
	case "webauthn":
		verification, err = h.mfaService.VerifyWebAuthnAssertion(
			user,
			[]byte(sessionData),
			[]byte(assertionResponse),
			ipAddress,
			userAgent,
			nil,
		)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_method"})
		return nil, fmt.Errorf("invalid_method")
	}

	if err != nil {
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(user.Email, user.Name, ipAddress, userAgent)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_code"})
		return nil, err
	}

	return verification, nil
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
	req, ok := h.bindCLILoginComplete(c)
	if !ok {
		return
	}

	record, ok := h.loadCLILoginState(c, req.State)
	if !ok {
		return
	}

	if handled := h.handleCLIRecordErrors(c, record); handled {
		return
	}

	if h.respondIfPending(c, record) {
		return
	}

	userRecord, ok := h.loadUserForCLI(c, record)
	if !ok {
		return
	}

	sessionToken, session, ok := h.createCLISessionOrRespond(c, userRecord, req)
	if !ok {
		return
	}

	h.applyMFATimestamps(record, session)

	response := h.buildCLILoginResponse(userRecord, record, sessionToken, session)
	_ = h.database.DeleteCLILoginState(req.State)
	h.notifyCLILoginComplete(c, userRecord)
	c.JSON(http.StatusOK, response)
}
