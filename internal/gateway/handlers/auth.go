package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// OAuthProvider captures the behaviour needed from the OAuth handler.
type OAuthProvider interface {
	StartLogin() (*auth.LoginStartResponse, error)
	StartWebLogin() (authURL string, state string)
	CompleteLogin(code, state, codeVerifier string) (*auth.LoginResponse, error)
}

const webOAuthStateCookie = "cgw_oauth_state"
const webOAuthStateTTL = 5 * time.Minute
const trustedDeviceCookie = "cgw_trusted_device"
const cliEnrollmentErrorMessage = "You must set up multi-factor authentication before you can continue using the CLI."

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	oauth            OAuthProvider
	database         *db.Database
	config           *config.Config
	sessions         *auth.SessionManager
	mfaService       *mfa.Service
	mfaSettings      *db.MFASettings
	securityNotifier *security.Notifier
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(oauth OAuthProvider, database *db.Database, cfg *config.Config, sessions *auth.SessionManager, mfaService *mfa.Service, mfaSettings *db.MFASettings, securityNotifier *security.Notifier) *AuthHandler {
	return &AuthHandler{
		oauth:            oauth,
		database:         database,
		config:           cfg,
		sessions:         sessions,
		mfaService:       mfaService,
		mfaSettings:      mfaSettings,
		securityNotifier: securityNotifier,
	}
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

	// Store the auth code and optional inline TOTP in database
	if h.database != nil {
		if err := h.database.UpdateCLILoginCode(state, code); err != nil {
			c.String(http.StatusInternalServerError, "Failed to persist login state")
			return
		}
	}

	redirect := fmt.Sprintf("/.gateway/api/auth/cli/mfa?state=%s", url.QueryEscape(state))
	c.Redirect(http.StatusTemporaryRedirect, redirect)
}

func (h *AuthHandler) CLILoginMFAForm(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	challengeRoute := WebRoute("auth/mfa/challenge")
	buildChallengeURL := func(params url.Values) string {
		if params == nil {
			params = url.Values{}
		}
		params.Set("channel", "cli")
		return fmt.Sprintf("%s?%s", challengeRoute, params.Encode())
	}

	if state == "" {
		params := url.Values{}
		params.Set("error", "missing_state")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}
	if h.database == nil {
		params := url.Values{}
		params.Set("error", "service_unavailable")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}

	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		params := url.Values{}
		params.Set("error", "load_failure")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}
	if record == nil {
		params := url.Values{}
		params.Set("error", "expired")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}

	if record.LoginError.Valid {
		params := url.Values{}
		params.Set("error", strings.TrimSpace(record.LoginError.String))
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
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
			c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
			return
		}

		loginResp, exchangeErr := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
		if exchangeErr != nil {
			params := url.Values{}
			params.Set("error", "exchange_failed")
			c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
			return
		}

		if err := h.database.SetCLILoginProfile(state, loginResp.Token, loginResp.Email, loginResp.Name, loginResp.ExpiresAt); err != nil {
			params := url.Values{}
			params.Set("error", "persist_failure")
			c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
			return
		}

		loginEmail = strings.TrimSpace(loginResp.Email)
	}

	if loginEmail == "" {
		if err := h.database.FailCLILoginState(state, "Unable to determine account information for CLI login."); err != nil {
			log.Printf("cli login fail (missing email): state=%s err=%v", state, err)
		}
		params := url.Values{}
		params.Set("error", "unauthorized")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}

	userRecord, err := h.database.GetUser(loginEmail)
	if err != nil {
		params := url.Values{}
		params.Set("error", "load_failure")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}
	if userRecord == nil {
		if err := h.database.FailCLILoginState(state, "User not authorized for this gateway."); err != nil {
			log.Printf("cli login fail (unknown user): state=%s err=%v", state, err)
		}
		params := url.Values{}
		params.Set("error", "unauthorized")
		c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
		return
	}

	if !shouldEnforceMFA(h.mfaSettings, userRecord) {
		if err := h.database.MarkCLILoginVerified(state, nil); err != nil {
			log.Printf("cli login mark verified failed: state=%s err=%v", state, err)
			params := url.Values{}
			params.Set("error", "persist_failure")
			c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, WebRoute("cli/auth/success"))
		return
	}

	if !userRecord.MFAEnrolled {
		if err := h.database.FailCLILoginState(state, cliEnrollmentErrorMessage); err != nil {
			log.Printf("cli login fail (enrollment required): state=%s err=%v", state, err)
		}

		if h.sessions != nil {
			sessionToken, _, createErr := h.sessions.CreateSession(userRecord, auth.SessionMetadata{
				Channel:   "web",
				IPAddress: c.ClientIP(),
				UserAgent: c.GetHeader("User-Agent"),
				Extra: map[string]interface{}{
					"login_flow": "cli-enrollment",
				},
			})
			if createErr == nil {
				h.setSessionCookie(c, sessionToken)
				// Leave MFAVerifiedAt unset; the UI will gate access until enrollment completes.
			} else {
				log.Printf("cli enrollment session create failed: user=%s err=%v", userRecord.Email, createErr)
			}
		}

		enrollParams := url.Values{}
		enrollParams.Set("enrollment", "required")
		enrollParams.Set("channel", "cli")
		enrollParams.Set("state", state)
		c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s?%s", WebRoute("account/security"), enrollParams.Encode()))
		return
	}

	params := url.Values{}
	params.Set("state", state)
	c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(params))
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
		if record.Code.Valid && record.CodeVerifier.Valid {
			loginResp, exchangeErr := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
			if exchangeErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "exchange_failed"})
				return
			}
			if err := h.database.SetCLILoginProfile(state, loginResp.Token, loginResp.Email, loginResp.Name, loginResp.ExpiresAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "persist_failure"})
				return
			}
			record.LoginEmail.String = loginResp.Email
			record.LoginEmail.Valid = true
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_incomplete"})
			return
		}
	}

	userRecord, err := h.database.GetUser(strings.TrimSpace(record.LoginEmail.String))
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Verify with appropriate method
	var verification *mfa.VerificationResult
	switch method {
	case "totp":
		verification, err = h.mfaService.VerifyTOTP(userRecord, strings.TrimSpace(req.Code))
	case "webauthn":
		verification, err = h.mfaService.VerifyWebAuthnAssertion(userRecord, []byte(req.SessionData), []byte(req.AssertionResponse))
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_method"})
		return
	}

	if err != nil {
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(userRecord.Email, userRecord.Name, c.ClientIP(), c.GetHeader("User-Agent"))
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
// @Success 200 {object} auth.LoginResponse
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
			h.securityNotifier.LoginAttempt(record.LoginEmail.String, userName, "cli", "user_not_authorized", c.ClientIP(), c.GetHeader("User-Agent"), false)
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
		if err := h.sessions.UpdateSessionMFAVerified(session.ID, record.MFAVerifiedAt.Time, nil); err != nil {
			log.Printf("failed to stamp session MFA verification: %v", err)
		} else {
			session.MFAVerifiedAt = &record.MFAVerifiedAt.Time
			if err := h.sessions.UpdateSessionRecentStepUp(session.ID, record.MFAVerifiedAt.Time); err != nil {
				log.Printf("failed updating session step-up timestamp: %v", err)
			} else {
				session.RecentStepUpAt = &record.MFAVerifiedAt.Time
			}
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
		h.securityNotifier.LoginAttempt(userRecord.Email, userRecord.Name, "cli", "complete", c.ClientIP(), c.GetHeader("User-Agent"), true)
	}

	c.JSON(http.StatusOK, response)
}

// WebLoginStart godoc
// @Summary Start web OAuth login
// @Description Redirects the browser to the identity provider for login.
// @Tags Auth
// @Success 302 {string} string "Redirect to identity provider"
// @Router /auth/web/login [get]
func (h *AuthHandler) WebLoginStart(c *gin.Context) {
	h.auditLogin(c, "web", "success")
	authURL, state := h.oauth.StartWebLogin()
	h.setWebOAuthStateCookie(c, state)
	c.Redirect(http.StatusFound, authURL)
}

// WebLoginCallback godoc
// @Summary Complete web OAuth login
// @Description Validates the OAuth callback, issues a session cookie, and redirects to the SPA.
// @Tags Auth
// @Param code query string true "Authorization code"
// @Param state query string true "State"
// @Success 302 {string} string "Redirect to web UI"
// @Failure 400 {string} string "Invalid state"
// @Failure 401 {string} string "User not authorized"
// @Failure 500 {string} string "Authentication failure"
// @Router /auth/web/callback [get]
func (h *AuthHandler) WebLoginCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.String(http.StatusBadRequest, "Missing authorization code or state")
		return
	}

	cookie, err := c.Request.Cookie(webOAuthStateCookie)
	if err != nil || cookie == nil || cookie.Value == "" {
		c.String(http.StatusBadRequest, "Invalid OAuth state")
		return
	}
	defer h.clearWebOAuthStateCookie(c)
	if subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) != 1 {
		c.String(http.StatusBadRequest, "Invalid OAuth state")
		return
	}

	// Web flow doesn't use PKCE (code_verifier is empty)
	resp, err := h.oauth.CompleteLogin(code, state, "")
	if err != nil {
		// Notify about failed login
		if h.securityNotifier != nil {
			h.securityNotifier.LoginAttempt("", "", "web", "oauth_failed", c.ClientIP(), c.GetHeader("User-Agent"), false)
		}
		c.String(http.StatusInternalServerError, "Authentication failed")
		return
	}

	if h.sessions == nil {
		c.String(http.StatusInternalServerError, "Session manager not available")
		return
	}

	userRecord, err := h.database.GetUser(resp.Email)
	if err != nil || userRecord == nil {
		// Notify about unauthorized login attempt
		if h.securityNotifier != nil {
			h.securityNotifier.LoginAttempt(resp.Email, resp.Name, "web", "user_not_authorized", c.ClientIP(), c.GetHeader("User-Agent"), false)
		}
		c.String(http.StatusUnauthorized, "User not authorized")
		return
	}

	extra := map[string]interface{}{"login_flow": "web"}
	sessionToken, session, err := h.sessions.CreateSession(userRecord, auth.SessionMetadata{
		Channel:    "web",
		IPAddress:  c.ClientIP(),
		UserAgent:  c.GetHeader("User-Agent"),
		Extra:      extra,
		DeviceName: "browser",
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to create session")
		return
	}

	if err := h.handlePostLoginMFA(c, userRecord, session); err != nil {
		log.Printf("post-login mfa (web) failed: user=%s session=%d err=%v", userRecord.Email, session.ID, err)
		params := url.Values{}
		params.Set("reason", "mfa-finalize")
		params.Set("message", "Failed to finalize login")
		errorURL := fmt.Sprintf("%s?%s", WebLoginErrorRoute, params.Encode())
		c.Redirect(http.StatusFound, errorURL)
		return
	}
	h.setSessionCookie(c, sessionToken)

	requireMFA := h.isMFARequired(userRecord)
	if requireMFA && session.MFAVerifiedAt == nil {
		params := url.Values{}
		params.Set("channel", "web")
		params.Set("redirect", DefaultWebRoute)
		challengeURL := fmt.Sprintf("%s?%s", WebRoute("auth/mfa/challenge"), params.Encode())
		c.Redirect(http.StatusFound, challengeURL)
		return
	}
	// Notify about successful login (includes audit logging)
	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(resp.Email, resp.Name, "web", "complete", c.ClientIP(), c.GetHeader("User-Agent"), true)
	}

	// Redirect to web UI
	c.Redirect(http.StatusFound, DefaultWebRoute)
}

// WebLogout godoc
// @Summary Log out current session
// @Description Revokes the active session, clears the cookie, and redirects to the login screen.
// @Tags Auth
// @Success 302 {string} string "Redirect to login"
// @Router /auth/web/logout [get]
func (h *AuthHandler) WebLogout(c *gin.Context) {
	h.clearWebOAuthStateCookie(c)
	sessionToken := extractSessionToken(c)
	if sessionToken != "" && h.sessions != nil {
		_, _ = h.sessions.RevokeByToken(sessionToken, nil)
	}
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"session_token",
		"",
		-1,
		"/",
		"",
		secure,
		true,
	)
	c.SetSameSite(http.SameSiteDefaultMode)

	// Audit logout
	if h.database != nil {
		if err := audit.LogDB(h.database, &db.AuditLog{
			ActionType:   "auth",
			Action:       "logout",
			ResourceType: "auth",
			Resource:     "web",
			Status:       "success",
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":"logout","error":%q}`, err)
		}
	}

	c.Redirect(http.StatusFound, "/.gateway/web/login")
}

func extractSessionToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if cookie, err := c.Cookie("session_token"); err == nil {
		if trimmed := strings.TrimSpace(cookie); trimmed != "" {
			return trimmed
		}
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		const bearerPrefix = "Bearer "
		if len(authHeader) >= len(bearerPrefix) && strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			return strings.TrimSpace(authHeader[len(bearerPrefix):])
		}
	}
	return ""
}

func (h *AuthHandler) setSessionCookie(c *gin.Context, value string) {
	secure := h.cookieSecure()
	maxAge := 0
	if ttl := h.sessionsTTL(); ttl > 0 {
		// Keep the cookie as a session cookie to avoid forcing logouts while active.
		// Sliding expiration is enforced server-side via the session manager.
		maxAge = 0
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"session_token",
		value,
		maxAge,
		"/",
		"",
		secure,
		true,
	)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) sessionsTTL() time.Duration {
	if h.sessions == nil {
		return 30 * 24 * time.Hour
	}
	return h.sessions.TTL()
}

func (h *AuthHandler) setWebOAuthStateCookie(c *gin.Context, value string) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	maxAge := int((webOAuthStateTTL) / time.Second)
	c.SetCookie(webOAuthStateCookie, value, maxAge, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) clearWebOAuthStateCookie(c *gin.Context) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(webOAuthStateCookie, "", -1, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) cookieSecure() bool {
	defaultSecure := h == nil || h.config == nil || !h.config.DevMode
	if v := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return defaultSecure
}

// Helper to audit login attempts
func (h *AuthHandler) auditLogin(c *gin.Context, resource, status string) {
	if h.database == nil {
		return
	}

	if err := audit.LogDB(h.database, &db.AuditLog{
		ActionType:   "auth",
		Action:       "login.start",
		ResourceType: "auth",
		Resource:     resource,
		Status:       status,
		IPAddress:    c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
	}); err != nil {
		log.Printf(`{"level":"error","event":"audit_log_failed","action":"login.start","error":%q}`, err)
	}
}

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

	result, err := h.mfaService.StartTOTPEnrollment(userRecord)
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
	if h.mfaService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "mfa service unavailable"})
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

	if err := h.mfaService.ConfirmTOTP(userRecord, req.MethodID, strings.TrimSpace(req.Code)); err != nil {
		// Notify about failed MFA enrollment attempt
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(userRecord.Email, userRecord.Name, c.ClientIP(), c.GetHeader("User-Agent"))
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

	// Audit log for MFA enrollment completion
	if h.database != nil {
		methodLabel := strings.TrimSpace(req.Label)
		if methodLabel == "" {
			methodLabel = "Authenticator App"
		}
		details, _ := json.Marshal(map[string]interface{}{
			"label": methodLabel,
		})
		if err := audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    userRecord.Email,
			UserName:     userRecord.Name,
			ActionType:   "auth",
			Action:       "mfa.enroll",
			ResourceType: "mfa_method",
			Resource:     "totp",
			Details:      string(details),
			Status:       "success",
			IPAddress:    c.ClientIP(),
			UserAgent:    c.GetHeader("User-Agent"),
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":"mfa.enroll","error":%q}`, err)
		}
	}

	response := VerifyMFAResponse{
		MFAVerifiedAt:         now,
		RecentStepUpExpiresAt: now.Add(h.stepUpWindow()),
		TrustedDeviceCookie:   trustedCookieSet,
	}
	c.JSON(http.StatusOK, response)
}

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

	if _, err := h.mfaService.VerifyTOTP(userRecord, strings.TrimSpace(req.Code)); err != nil {
		// Notify about failed MFA attempt
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(userRecord.Email, userRecord.Name, c.ClientIP(), c.GetHeader("User-Agent"))
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

	if _, err := h.mfaService.VerifyWebAuthnAssertion(userRecord, []byte(req.SessionData), []byte(req.AssertionResponse)); err != nil {
		// Notify about failed MFA attempt
		if h.securityNotifier != nil {
			h.securityNotifier.FailedMFAAttempt(userRecord.Email, userRecord.Name, c.ClientIP(), c.GetHeader("User-Agent"))
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

// RegenerateBackupCodes godoc
// @Summary Regenerate backup codes
// @Description Generates a fresh set of backup codes. Existing codes are invalidated immediately.
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
		if err := audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    userRecord.Email,
			UserName:     userRecord.Name,
			ActionType:   "auth",
			Action:       "mfa.update",
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

	var req StartYubiOTPEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf(`{"level":"error","event":"yubiotp_bind_failed","error":%q}`, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	result, err := h.mfaService.StartYubiOTPEnrollment(userRecord, req.YubiOTP)
	if err != nil {
		log.Printf(`{"level":"error","event":"yubiotp_enrollment_failed","user":%q,"error":%q}`, authUser.Email, err.Error())
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

	result, sessionData, err := h.mfaService.StartWebAuthnEnrollment(userRecord)
	if err != nil {
		log.Printf(`{"level":"error","event":"webauthn_start_failed","user":%q,"error":%q}`, authUser.Email, err.Error())
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
	log.Printf("WebAuthn enrollment start - MethodID: %d, PublicKeyOptions JSON: %s", result.MethodID, string(optionsJSON))

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
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /auth/mfa/enroll/webauthn/confirm [post]
func (h *AuthHandler) ConfirmWebAuthnEnrollment(c *gin.Context) {
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

	methodID, err := h.mfaService.ConfirmWebAuthnEnrollment(userRecord, []byte(sessionDataStr), credentialJSON, label)
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

	// Audit log for WebAuthn enrollment completion
	if h.database != nil {
		details, _ := json.Marshal(map[string]interface{}{
			"label": label,
		})
		if err := audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    userRecord.Email,
			UserName:     userRecord.Name,
			ActionType:   "auth",
			Action:       "mfa.enroll",
			ResourceType: "mfa_method",
			Resource:     "webauthn",
			Details:      string(details),
			Status:       "success",
			IPAddress:    c.ClientIP(),
			UserAgent:    c.GetHeader("User-Agent"),
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":"mfa.enroll","error":%q}`, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "enrolled", "method_id": methodID})
}

// RevokeTrustedDevice godoc
// @Summary Revoke a trusted device
// @Description Revokes a trusted device token for the current user.
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

func makeMFAMethodResponse(method *db.MFAMethod) MFAMethodResponse {
	if method == nil {
		return MFAMethodResponse{}
	}
	label := strings.TrimSpace(method.Label)
	if label == "" {
		label = strings.ToUpper(strings.TrimSpace(method.Type))
	}
	resp := MFAMethodResponse{
		ID:        method.ID,
		Type:      method.Type,
		Label:     truncateLabel(label, 120),
		CreatedAt: method.CreatedAt,
	}
	if method.ConfirmedAt != nil {
		confirmed := *method.ConfirmedAt
		resp.ConfirmedAt = &confirmed
	}
	if method.LastUsedAt != nil {
		last := *method.LastUsedAt
		resp.LastUsedAt = &last
	}
	return resp
}

func makeTrustedDeviceResponse(device *db.TrustedDevice) TrustedDeviceResponse {
	if device == nil {
		return TrustedDeviceResponse{}
	}
	ua := extractTrustedDeviceUserAgent(device)
	label := ua
	if label == "" {
		label = fmt.Sprintf("Device %s", shortDeviceID(device.DeviceID))
	}
	ip := strings.TrimSpace(device.IPLast)
	if ip == "" {
		ip = strings.TrimSpace(device.IPFirst)
	}
	resp := TrustedDeviceResponse{
		ID:        device.ID,
		Label:     truncateLabel(label, 160),
		CreatedAt: device.CreatedAt,
		ExpiresAt: device.ExpiresAt,
		IPAddress: ip,
		UserAgent: truncateLabel(ua, 200),
	}
	last := device.LastUsedAt
	resp.LastUsedAt = &last
	if device.RevokedAt != nil {
		resp.RevokedAt = device.RevokedAt
	}
	if reason := strings.TrimSpace(device.RevokedReason); reason != "" {
		resp.RevokedReason = reason
	}
	return resp
}

func extractTrustedDeviceUserAgent(device *db.TrustedDevice) string {
	if device == nil || len(device.Metadata) == 0 {
		return ""
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(device.Metadata, &meta); err != nil {
		return ""
	}
	if ua, ok := meta["user_agent"].(string); ok {
		return strings.TrimSpace(ua)
	}
	return ""
}

func shortDeviceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func truncateLabel(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func summarizeBackupCodes(codes []*db.MFABackupCode) MFABackupCodesSummary {
	summary := MFABackupCodesSummary{Total: len(codes)}
	for _, code := range codes {
		if code == nil {
			continue
		}
		created := code.CreatedAt
		if summary.LastGeneratedAt == nil || created.After(*summary.LastGeneratedAt) {
			createdCopy := created
			summary.LastGeneratedAt = &createdCopy
		}
		if code.UsedAt == nil {
			summary.Unused++
			continue
		}
		used := *code.UsedAt
		if summary.LastUsedAt == nil || used.After(*summary.LastUsedAt) {
			usedCopy := used
			summary.LastUsedAt = &usedCopy
		}
	}
	return summary
}

func (h *AuthHandler) handlePostLoginMFA(c *gin.Context, user *db.User, session *db.UserSession) error {
	if h.sessions == nil || user == nil || session == nil {
		return nil
	}

	if !h.isMFARequired(user) {
		now := time.Now()
		if err := h.sessions.UpdateSessionMFAVerified(session.ID, now, nil); err != nil {
			return fmt.Errorf("mark session verified: %w", err)
		}
		session.MFAVerifiedAt = &now
		return nil
	}

	trustedDevice, err := h.consumeTrustedDevice(c, user)
	if err != nil {
		log.Printf("post-login mfa: consume trusted device failed for user=%s: %v", user.Email, err)
		h.clearTrustedDeviceCookie(c)
		return nil
	}
	if trustedDevice == nil {
		return nil
	}

	now := time.Now()
	if err := h.sessions.UpdateSessionMFAVerified(session.ID, now, &trustedDevice.ID); err != nil {
		return fmt.Errorf("update session with trusted device: %w", err)
	}
	session.MFAVerifiedAt = &now
	session.TrustedDeviceID = &trustedDevice.ID
	return nil
}

func (h *AuthHandler) consumeTrustedDevice(c *gin.Context, user *db.User) (*db.TrustedDevice, error) {
	if h.mfaService == nil || user == nil {
		return nil, nil
	}

	cookie, err := c.Request.Cookie(trustedDeviceCookie)
	if err != nil {
		return nil, nil
	}
	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return nil, nil
	}

	device, err := h.mfaService.ConsumeTrustedDevice(token, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.clearTrustedDeviceCookie(c)
		return nil, err
	}
	if device.UserID != user.ID {
		_ = h.database.RevokeTrustedDevice(device.ID, "mismatched_user")
		h.clearTrustedDeviceCookie(c)
		return nil, fmt.Errorf("trusted device mismatch")
	}

	return device, nil
}

func (h *AuthHandler) setTrustedDeviceCookie(c *gin.Context, token string) {
	secure := h.cookieSecure()
	maxAge := h.trustedDeviceMaxAge()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(trustedDeviceCookie, token, maxAge, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) clearTrustedDeviceCookie(c *gin.Context) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(trustedDeviceCookie, "", -1, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) isMFARequired(user *db.User) bool {
	return isMFAChallengeRequired(h.mfaSettings, user)
}

func (h *AuthHandler) stepUpWindow() time.Duration {
	window := 10 * time.Minute
	if h.mfaSettings != nil && h.mfaSettings.StepUpWindowMinutes > 0 {
		window = time.Duration(h.mfaSettings.StepUpWindowMinutes) * time.Minute
	}
	return window
}

func (h *AuthHandler) trustedDeviceMaxAge() int {
	ttl := 30 * 24 * time.Hour
	if h.mfaSettings != nil && h.mfaSettings.TrustedDeviceTTLDays > 0 {
		ttl = time.Duration(h.mfaSettings.TrustedDeviceTTLDays) * 24 * time.Hour
	}
	return int(ttl.Seconds())
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
