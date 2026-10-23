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
)

func (h *AuthHandler) handleMFARedirect(
	c *gin.Context,
	userRecord *db.User,
	session *db.UserSession,
	redirectURL string,
) bool {
	enforceMFA := db.ShouldEnforceMFA(h.mfaSettings(), userRecord)
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
	if !h.auditLogin(c, "web", "success") {
		redirectToWebLoginError(c, webLoginAuditUnavailable)
		return
	}

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
		redirectToWebLoginError(c, webLoginProviderError(oauthError))
		return
	}

	// Validate code and state parameters
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		redirectToWebLoginError(c, webLoginMissingCode)
		return
	}

	// Validate OAuth state cookie
	if !h.validateWebOAuthState(c, state) {
		redirectToWebLoginError(c, webLoginStateMismatch)
		return
	}

	// Complete OAuth login
	resp, err := h.oauth.CompleteLogin(c.Request.Context(), code, state, "")
	if err != nil {
		h.handleWebLoginOAuthError(c, err)
		return
	}

	// Validate session manager is available
	if h.sessions == nil {
		redirectToWebLoginError(c, webLoginSessionFailed)
		return
	}

	// Validate user exists and is authorized
	userRecord, err := h.database.GetUser(resp.Email)
	if err != nil || userRecord == nil {
		h.handleWebLoginUnauthorized(c, resp.Email, resp.Name)
		return
	}
	if ok, err := h.bindSignInAccount(c, userRecord, resp.Subject, resp.Name, "web"); !ok {
		reason := webLoginAccountMismatch
		if err != nil {
			reason = webLoginSessionFailed
		}
		redirectToWebLoginError(c, reason)
		return
	}
	h.fillUserName(userRecord, resp.Name)

	if !h.recordAuthAttempt(c, audit.ActionSessionCreate, "web", userRecord.Email, userRecord.Name) {
		redirectToWebLoginError(c, webLoginAuditUnavailable)
		return
	}
	session, err := h.createLoginSession(c, userRecord, "web")
	h.recordAuthResult(c, audit.ActionSessionCreate, "web", userRecord.Email, userRecord.Name, auditStatus(err))
	if err != nil {
		redirectToWebLoginError(c, webLoginSessionFailed)
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
// @Description Revokes the active session and clears the cookie. A POST with the session's CSRF token, so another
// @Description site can't sign the user out. Fails with 500, keeping the cookie, when the session couldn't be
// @Description revoked, so the user can try again. A request without a live session just clears the cookie.
// @Tags Auth
// @Param X-CSRF-Token header string true "The session's CSRF token"
// @Success 200 {object} WebLogoutResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /auth/web/logout [post]
func (h *AuthHandler) WebLogout(c *gin.Context) {
	h.clearWebOAuthStateCookie(c)
	sessionToken := extractSessionToken(c)
	email, name := h.sessionUser(c, sessionToken)
	if email != "" && !h.sessions.ValidateCSRFToken(sessionToken, strings.TrimSpace(c.GetHeader("X-CSRF-Token"))) {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
		return
	}
	if !h.recordAuthAttempt(c, audit.ActionScopeLogout, "web", email, name) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": audit.RefusalMessage(audit.ActionScopeLogout)})
		return
	}

	var revokeErr error
	if email != "" {
		_, revokeErr = h.sessions.RevokeByToken(sessionToken, nil)
	}
	h.recordAuthResult(c, audit.ActionScopeLogout, "web", email, name, auditStatus(revokeErr))
	if revokeErr != nil {
		log.Printf("web logout: failed to revoke the session of %s: %v", email, revokeErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "logout_failed"})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("session_token", "", -1, "/", "", h.cookieSecure(), true)
	c.SetSameSite(http.SameSiteDefaultMode)
	c.JSON(http.StatusOK, WebLogoutResponse{LoggedOut: true})
}

// WebLogoutResponse is the answer to a completed web logout.
type WebLogoutResponse struct {
	LoggedOut bool `json:"logged_out"`
}

// sessionUser names the user a session token belongs to. A token that isn't a live session (or no session manager)
// names nobody.
func (h *AuthHandler) sessionUser(c *gin.Context, sessionToken string) (email, name string) {
	if sessionToken == "" || h.sessions == nil {
		return "", ""
	}
	result, err := h.sessions.ValidateSession(sessionToken, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil || result == nil || result.User == nil {
		return "", ""
	}
	return result.User.Email, result.User.Name
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
	reason := webLoginExchangeFailed

	if errors.As(err, &domainErr) {
		email = domainErr.Email
		name = domainErr.Name
		reason = webLoginUnauthorized
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
	redirectToWebLoginError(c, reason)
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
	redirectToWebLoginError(c, webLoginUnauthorized)
}
