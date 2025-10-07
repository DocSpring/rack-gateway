package handlers

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"net/url"

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

	session, err := h.createLoginSession(c, userRecord, "web")
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to create session")
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
