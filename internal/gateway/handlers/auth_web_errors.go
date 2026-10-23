package handlers

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// webLoginError is why a web login failed. The login error page (web/src/pages/login-error-page.tsx) shows a fixed
// message for each one and never text from its URL, so a crafted link can't put its own words, such as a phone
// number to call, on the gateway's page. Neither the identity provider's error description nor any other text
// from a request is passed on.
type webLoginError string

const (
	// webLoginCanceled is the identity provider's access_denied: the user canceled or declined the sign-in.
	webLoginCanceled webLoginError = "access_denied"
	// webLoginIdentityProviderError is any other error the identity provider sent back.
	webLoginIdentityProviderError webLoginError = "identity_provider_error"
	// webLoginMissingCode is a callback without an authorization code or state.
	webLoginMissingCode webLoginError = "missing_code"
	// webLoginStateMismatch is a callback whose state isn't the one this browser's login started with.
	webLoginStateMismatch webLoginError = "state_mismatch"
	// webLoginExchangeFailed is a code the identity provider wouldn't exchange for the user's identity.
	webLoginExchangeFailed webLoginError = "exchange_failed"
	// webLoginUnauthorized is a user who isn't allowed to use this gateway.
	webLoginUnauthorized webLoginError = "unauthorized"
	// webLoginAccountMismatch is an account signing in with a user's email that isn't the account bound to the
	// user (db.BindUserSignInAccount).
	webLoginAccountMismatch webLoginError = errCodeAccountMismatch
	// webLoginSessionFailed is a login the gateway couldn't start a session for.
	webLoginSessionFailed webLoginError = "session_failed"
	// webLoginAuditUnavailable is a login refused because the audit log couldn't record it.
	webLoginAuditUnavailable webLoginError = "audit_unavailable"
)

// webLoginProviderError is the reason for an error the identity provider sent back.
func webLoginProviderError(providerError string) webLoginError {
	if providerError == string(webLoginCanceled) {
		return webLoginCanceled
	}
	return webLoginIdentityProviderError
}

// redirectToWebLoginError sends the browser to the login error page, which explains reason.
func redirectToWebLoginError(c *gin.Context, reason webLoginError) {
	c.Redirect(http.StatusFound, WebLoginErrorRoute+"?"+url.Values{"error": {string(reason)}}.Encode())
}
