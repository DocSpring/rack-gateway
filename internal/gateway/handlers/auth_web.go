package handlers

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

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
	// Check for OAuth error response (user cancelled or other OAuth error)
	if oauthError := c.Query("error"); oauthError != "" {
		errorDesc := c.Query("error_description")
		if errorDesc == "" {
			errorDesc = "Authentication was cancelled or failed"
		}
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape(errorDesc))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape("Missing authorization code or state"))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	cookie, err := c.Request.Cookie(webOAuthStateCookie)
	if err != nil || cookie == nil || cookie.Value == "" {
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape("Invalid OAuth state"))
		c.Redirect(http.StatusFound, errorURL)
		return
	}
	defer h.clearWebOAuthStateCookie(c)
	if subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) != 1 {
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape("Invalid OAuth state"))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	// Web flow doesn't use PKCE (code_verifier is empty)
	resp, err := h.oauth.CompleteLogin(code, state, "")
	if err != nil {
		// Extract user info from error if available
		var domainErr *auth.DomainNotAllowedError
		email := ""
		name := ""
		errorMsg := "Authentication failed. Please try again."

		if errors.As(err, &domainErr) {
			email = domainErr.Email
			name = domainErr.Name
			errorMsg = "You are not authorized to access this application. Please contact your administrator."
		}

		// Notify about failed login (with email/name if available)
		if h.securityNotifier != nil {
			h.securityNotifier.LoginAttempt(email, name, "web", "oauth_failed", c.ClientIP(), c.GetHeader("User-Agent"), false)
		}
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape(errorMsg))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	if h.sessions == nil {
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape("Session manager not available"))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	userRecord, err := h.database.GetUser(resp.Email)
	if err != nil || userRecord == nil {
		// Notify about unauthorized login attempt
		if h.securityNotifier != nil {
			h.securityNotifier.LoginAttempt(resp.Email, resp.Name, "web", "user_not_authorized", c.ClientIP(), c.GetHeader("User-Agent"), false)
		}
		errorMsg := "You are not authorized to access this application. Please contact your administrator."
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape(errorMsg))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	session, err := h.createLoginSession(c, userRecord, "web")
	if err != nil {
		errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape("Failed to create session. Please try again."))
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	// Check if MFA verification is required
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
		if err := h.auditLogger.LogDBEntry(&db.AuditLog{
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
