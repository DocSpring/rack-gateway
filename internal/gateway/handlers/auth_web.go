package handlers

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *AuthHandler) handleMFARedirect(
	c *gin.Context,
	userRecord *db.User,
	session *db.UserSession,
	redirectURL string,
) bool {
	enforceMFA := shouldEnforceMFA(h.mfaSettings, userRecord)
	if !enforceMFA {
		return false
	}

	// If user is not enrolled yet, redirect to enrollment page
	if !userRecord.MFAEnrolled {
		params := url.Values{}
		params.Set("redirect", redirectURL)
		enrollmentURL := fmt.Sprintf("%s?%s", WebRoute("account/security"), params.Encode())
		c.Redirect(http.StatusFound, enrollmentURL)
		return true
	}

	// If user is enrolled but not verified in this session, redirect to challenge page
	if session.MFAVerifiedAt == nil {
		params := url.Values{}
		params.Set("channel", "web")
		params.Set("redirect", redirectURL)
		challengeURL := fmt.Sprintf("%s?%s", WebRoute("auth/mfa/challenge"), params.Encode())
		c.Redirect(http.StatusFound, challengeURL)
		return true
	}

	return false
}

func (h *AuthHandler) getValidatedRedirectURL(c *gin.Context) string {
	returnTo := h.getReturnToCookie(c)

	redirectURL := DefaultWebRoute
	if returnTo != "" && validateReturnTo(returnTo) {
		redirectURL = returnTo
	}
	return redirectURL
}

// WebLoginStart godoc
// @Summary Start web OAuth login
// @Description Redirects the browser to the identity provider for login.
// @Tags Auth
// @Param returnTo query string false "URL to redirect to after successful login (must start with /app/)"
// @Success 302 {string} string "Redirect to identity provider"
// @Router /auth/web/login [get]
func (h *AuthHandler) WebLoginStart(c *gin.Context) {
	h.auditLogin(c, "web", "success")

	// Capture and validate returnTo parameter
	returnTo := strings.TrimSpace(c.Query("returnTo"))
	if returnTo != "" && validateReturnTo(returnTo) {
		h.setReturnToCookie(c, returnTo)
	}

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
	// Check for OAuth error response
	if oauthError := c.Query("error"); oauthError != "" {
		h.redirectToWebLoginError(c, h.getOAuthErrorMessage(c))
		return
	}

	// Validate code and state parameters
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		h.redirectToWebLoginError(c, "Missing authorization code or state")
		return
	}

	// Validate OAuth state cookie
	if !h.validateWebOAuthState(c, state) {
		h.redirectToWebLoginError(c, "Invalid OAuth state")
		return
	}

	// Complete OAuth login
	resp, err := h.oauth.CompleteLogin(code, state, "")
	if err != nil {
		h.handleWebLoginOAuthError(c, err)
		return
	}

	// Validate session manager is available
	if h.sessions == nil {
		h.redirectToWebLoginError(c, "Session manager not available")
		return
	}

	// Validate user exists and is authorized
	userRecord, err := h.database.GetUser(resp.Email)
	if err != nil || userRecord == nil {
		h.handleWebLoginUnauthorized(c, resp.Email, resp.Name)
		return
	}

	session, err := h.createLoginSession(c, userRecord, "web")
	if err != nil {
		errorURL := fmt.Sprintf(
			"%s?message=%s",
			WebLoginErrorRoute,
			url.QueryEscape("Failed to create session. Please try again."),
		)
		c.Redirect(http.StatusFound, errorURL)
		return
	}

	redirectURL := h.getValidatedRedirectURL(c)

	// Check if MFA redirect is needed
	if h.handleMFARedirect(c, userRecord, session, redirectURL) {
		return
	}

	// Notify about successful login (includes audit logging)
	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(
			resp.Email,
			resp.Name,
			"web",
			"complete",
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			true,
		)
	}

	// Clear the returnTo cookie now that we're done with it
	h.clearReturnToCookie(c)

	// Redirect to returnTo URL or default web route
	c.Redirect(http.StatusFound, redirectURL)
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
			ActionType:   audit.ActionTypeAuth,
			Action:       audit.ActionScopeLogout,
			ResourceType: rbac.ResourceAuth.String(),
			Resource:     "web",
			Status:       audit.StatusSuccess,
		}); err != nil {
			log.Printf(
				`{"level":"error","event":"audit_log_failed","action":%q,"error":%q}`,
				audit.ActionScopeLogout,
				err,
			)
		}
	}

	c.Redirect(http.StatusFound, "/app/login")
}

func (h *AuthHandler) getOAuthErrorMessage(c *gin.Context) string {
	errorDesc := c.Query("error_description")
	if errorDesc == "" {
		return "Authentication was canceled or failed"
	}
	return errorDesc
}

func (h *AuthHandler) redirectToWebLoginError(c *gin.Context, message string) {
	errorURL := fmt.Sprintf("%s?message=%s", WebLoginErrorRoute, url.QueryEscape(message))
	c.Redirect(http.StatusFound, errorURL)
}

func (h *AuthHandler) validateWebOAuthState(c *gin.Context, state string) bool {
	cookie, err := c.Request.Cookie(webOAuthStateCookie)
	if err != nil || cookie == nil || cookie.Value == "" {
		return false
	}
	defer h.clearWebOAuthStateCookie(c)
	return subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) == 1
}

func (h *AuthHandler) handleWebLoginOAuthError(c *gin.Context, err error) {
	var domainErr *auth.DomainNotAllowedError
	email := ""
	name := ""
	errorMsg := "Authentication failed. Please try again."

	if errors.As(err, &domainErr) {
		email = domainErr.Email
		name = domainErr.Name
		errorMsg = "You are not authorized to access this application. Please contact your administrator."
	}

	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(
			email,
			name,
			"web",
			"oauth_failed",
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			false,
		)
	}
	h.redirectToWebLoginError(c, errorMsg)
}

func (h *AuthHandler) handleWebLoginUnauthorized(c *gin.Context, email, name string) {
	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(
			email,
			name,
			"web",
			"user_not_authorized",
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			false,
		)
	}
	errorMsg := "You are not authorized to access this application. Please contact your administrator."
	h.redirectToWebLoginError(c, errorMsg)
}
