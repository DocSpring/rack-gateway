package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
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
// @Summary Start CLI login
// @Description Starts a loopback CLI login and returns the identity provider URL to open in the browser.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body CLILoginStartRequest true "CLI login parameters"
// @Success 200 {object} CLILoginStartResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/cli/start [post]
func (h *AuthHandler) CLILoginStart(c *gin.Context) {
	var req CLILoginStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	deviceName, err := validateCLIStart(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.database == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service_unavailable"})
		return
	}

	resp, err := h.oauth.StartLogin()
	if err != nil {
		h.auditLogin(c, "cli", "error")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.database.CreateCLILoginState(db.NewCLILogin{
		State:             resp.State,
		OAuthCodeVerifier: resp.CodeVerifier,
		CLICodeChallenge:  req.CodeChallenge,
		CLIRedirectURI:    req.RedirectURI,
		CLIState:          req.State,
		InitiatorIP:       c.ClientIP(),
		InitiatorDevice:   deviceName,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize login state"})
		return
	}

	h.auditLogin(c, "cli", "success")
	c.JSON(http.StatusOK, CLILoginStartResponse{AuthURL: resp.AuthURL})
}

// CLILoginCallback godoc
// @Summary Identity provider redirect for CLI login
// @Description Stores the authorization code and binds the login to this browser.
// @Tags Auth
// @Param code query string true "Authorization code"
// @Param state query string true "State"
// @Success 307 {string} string "Temporary Redirect"
// @Router /auth/cli/callback [get]
func (h *AuthHandler) CLILoginCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		cliRedirectWithError(c, "missing_state")
		return
	}
	if h.database == nil {
		cliRedirectWithError(c, "service_unavailable")
		return
	}

	binding, err := newCLISecret()
	if err != nil {
		cliRedirectWithError(c, "persist_failure")
		return
	}
	bound, err := h.database.BindCLILoginBrowser(state, code, hashCLISecret(binding))
	if err != nil {
		cliRedirectWithError(c, "load_failure")
		return
	}
	if !bound {
		cliRedirectWithError(c, "expired")
		return
	}

	h.setCLILoginCookie(c, binding, cliLoginCookieMaxAge)
	c.Redirect(http.StatusTemporaryRedirect, cliMFARoute(state))
}

// CLILoginMFAForm godoc
// @Summary Continue CLI login in the browser
// @Description Completes the identity provider exchange and sends the bound browser to MFA or back to the CLI.
// @Tags Auth
// @Param state query string true "State"
// @Success 307 {string} string "Temporary Redirect"
// @Router /auth/cli/mfa [get]
func (h *AuthHandler) CLILoginMFAForm(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	record, ok := h.liveBoundCLILogin(c, state)
	if !ok {
		return
	}
	if record.MFAVerifiedAt.Valid {
		c.Redirect(http.StatusTemporaryRedirect, cliReturnRoute(state))
		return
	}

	userRecord, ok := h.cliResolveUser(c, record)
	if !ok {
		return
	}

	if !shouldEnforceMFA(h.mfaSettings, userRecord) {
		if err := h.database.MarkCLILoginVerified(state, nil); err != nil {
			cliRedirectWithError(c, "persist_failure")
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, cliReturnRoute(state))
		return
	}

	if _, err := h.createLoginSession(c, userRecord, "cli-mfa"); err != nil {
		log.Printf("cli mfa session create failed: user=%s err=%v", userRecord.Email, err)
		cliRedirectWithError(c, "session_failed")
		return
	}

	if !userRecord.MFAEnrolled {
		h.cliRedirectToEnrollment(c, state)
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, cliChallengeURL(record))
}

// cliResolveUser exchanges the identity provider code (once) and loads the gateway user.
// On failure the login is ended and the browser is sent back to the CLI with the reason.
func (h *AuthHandler) cliResolveUser(c *gin.Context, record *db.CLILoginState) (*db.User, bool) {
	email := ""
	if record.LoginEmail.Valid {
		email = strings.TrimSpace(record.LoginEmail.String)
	}
	if email == "" {
		exchanged, errCode := h.cliExchangeOAuthCode(record)
		if errCode != "" {
			h.failCLILogin(c, record, errCode)
			return nil, false
		}
		email = exchanged
		record.LoginEmail.String, record.LoginEmail.Valid = exchanged, true
	}

	userRecord, err := h.database.GetUser(email)
	if err != nil {
		cliRedirectWithError(c, "load_failure")
		return nil, false
	}
	if userRecord == nil {
		h.notifyUnauthorizedCLILogin(c, record)
		h.failCLILogin(c, record, "unauthorized")
		return nil, false
	}
	return userRecord, true
}

// cliExchangeOAuthCode exchanges the stored identity provider code and records the profile.
// Returns the login email, or an error code for the CLI.
func (h *AuthHandler) cliExchangeOAuthCode(record *db.CLILoginState) (string, string) {
	if !record.OAuthCode.Valid || !record.OAuthCodeVerifier.Valid {
		return "", "session_incomplete"
	}
	loginResp, err := h.oauth.CompleteLogin(record.OAuthCode.String, record.State, record.OAuthCodeVerifier.String)
	if err != nil {
		var domainErr *auth.DomainNotAllowedError
		if errors.As(err, &domainErr) {
			return "", "unauthorized"
		}
		return "", "exchange_failed"
	}
	if err := h.database.SetCLILoginProfile(record.State, loginResp.Email, loginResp.Name); err != nil {
		return "", "persist_failure"
	}
	record.LoginName.String, record.LoginName.Valid = loginResp.Name, true
	return strings.TrimSpace(loginResp.Email), ""
}

func (h *AuthHandler) cliRedirectToEnrollment(c *gin.Context, state string) {
	if err := h.database.MarkCLILoginEnrollmentRequired(state); err != nil {
		cliRedirectWithError(c, "persist_failure")
		return
	}
	params := url.Values{}
	params.Set("enrollment", "required")
	params.Set("channel", "cli")
	params.Set("state", state)
	c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s?%s", WebRoute("account/security"), params.Encode()))
}

// cliChallengeURL builds the MFA challenge page URL, including where the login was started so the
// user can spot a login they did not start.
func cliChallengeURL(record *db.CLILoginState) string {
	params := url.Values{}
	params.Set("state", record.State)
	if record.InitiatorDevice.Valid && record.InitiatorDevice.String != "" {
		params.Set("device", record.InitiatorDevice.String)
	}
	if record.InitiatorIP.Valid && record.InitiatorIP.String != "" {
		params.Set("ip", record.InitiatorIP.String)
	}
	return buildChallengeURL(WebRoute("auth/mfa/challenge"), params)
}

// CLILoginMFASubmit handles both TOTP and WebAuthn verification for CLI login.
// Only the browser bound to the login may submit codes, so a third party who learns the
// login state cannot burn the user's MFA attempts.
func (h *AuthHandler) CLILoginMFASubmit(c *gin.Context) {
	if h.database == nil || h.mfaService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "service_unavailable"})
		return
	}

	parsed, ok := h.parseMFASubmitRequest(c)
	if !ok {
		return
	}

	record, errCode := h.loadBoundCLILogin(c, parsed.state)
	if errCode != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCode})
		return
	}
	if record.MFAVerifiedAt.Valid {
		c.JSON(http.StatusOK, gin.H{"redirect": cliReturnRoute(parsed.state)})
		return
	}
	if !record.LoginEmail.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_incomplete"})
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
	h.completeBrowserSessionMFA(c, userRecord)

	c.JSON(http.StatusOK, gin.H{"redirect": cliReturnRoute(parsed.state)})
}

// parseMFASubmitRequest parses and validates the MFA submit request payload.
func (_ *AuthHandler) parseMFASubmitRequest(c *gin.Context) (cliMFASubmit, bool) {
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
