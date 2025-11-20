package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// setReturnToCookie stores the returnTo path in a secure cookie for redirect after OAuth completion
func (h *AuthHandler) setReturnToCookie(c *gin.Context, value string) {
	secure := h.cookieSecure()
	maxAge := int(webOAuthStateTTL / time.Second)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(webOAuthReturnToCookie, value, maxAge, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

// getReturnToCookie retrieves the returnTo path from the secure cookie
func (h *AuthHandler) getReturnToCookie(c *gin.Context) string {
	cookie, err := c.Request.Cookie(webOAuthReturnToCookie)
	if err != nil || cookie == nil {
		return ""
	}
	// URL-decode the cookie value since gin may encode it
	decoded, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(decoded)
}

// clearReturnToCookie removes the returnTo cookie after it's been used
func (h *AuthHandler) clearReturnToCookie(c *gin.Context) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(webOAuthReturnToCookie, "", -1, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

// validateReturnTo ensures the returnTo path is safe to redirect to, preventing open redirect vulnerabilities.
// Only allows relative paths that start with /app/ to prevent redirecting to external sites.
func validateReturnTo(returnTo string) bool {
	if returnTo == "" {
		return false
	}
	// Must start with /app/ (the SPA base path)
	if !strings.HasPrefix(returnTo, "/app/") {
		return false
	}
	// Must not contain protocol or host (no :// patterns)
	if strings.Contains(returnTo, "://") {
		return false
	}
	// Must not start with // (protocol-relative URL)
	if strings.HasPrefix(returnTo, "//") {
		return false
	}
	// Must not contain backslashes (Windows path or escape sequences)
	if strings.Contains(returnTo, "\\") {
		return false
	}
	return true
}
