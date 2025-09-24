package handlers

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

// OAuthProvider captures the behaviour needed from the OAuth handler.
type OAuthProvider interface {
	StartLogin() (*auth.LoginStartResponse, error)
	StartWebLogin() (authURL string, state string)
	CompleteLogin(code, state, codeVerifier string) (*auth.LoginResponse, error)
}

const webOAuthStateCookie = "cgw_oauth_state"
const webOAuthStateTTL = 5 * time.Minute

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	oauth    OAuthProvider
	database *db.Database
	config   *config.Config
	sessions *auth.SessionManager
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(oauth OAuthProvider, database *db.Database, cfg *config.Config, sessions *auth.SessionManager) *AuthHandler {
	return &AuthHandler{
		oauth:    oauth,
		database: database,
		config:   cfg,
		sessions: sessions,
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
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.String(http.StatusBadRequest, "Missing code or state")
		return
	}

	// Store the auth code in database
	if h.database != nil {
		_ = h.database.SaveCLILoginCode(state, code)
	}

	// Redirect to a nicer static success page served by the web bundle
	c.Redirect(http.StatusTemporaryRedirect, "/.gateway/web/cli-auth-success.html")
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

	// Retrieve auth code from database
	code, exists, err := h.database.GetCLILoginCode(req.State)
	if err != nil || !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired state"})
		return
	}

	// Complete OAuth flow with PKCE
	resp, err := h.oauth.CompleteLogin(code, req.State, req.CodeVerifier)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Clear the stored code
	_ = h.database.DeleteCLILoginCode(req.State)

	c.JSON(http.StatusOK, resp)
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
		c.String(http.StatusInternalServerError, "Authentication failed")
		return
	}

	if h.sessions == nil {
		c.String(http.StatusInternalServerError, "Session manager not available")
		return
	}

	userRecord, err := h.database.GetUser(resp.Email)
	if err != nil || userRecord == nil {
		c.String(http.StatusUnauthorized, "User not authorized")
		return
	}

	sessionToken, _, err := h.sessions.CreateSession(userRecord, auth.SessionMetadata{
		IPAddress: c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to create session")
		return
	}

	h.setSessionCookie(c, sessionToken)

	// Audit successful login
	if h.database != nil {
		audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    resp.Email,
			ActionType:   "auth",
			Action:       "login.complete",
			ResourceType: "auth",
			Resource:     "web",
			Status:       "success",
		})
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
		audit.LogDB(h.database, &db.AuditLog{
			ActionType:   "auth",
			Action:       "logout",
			ResourceType: "auth",
			Resource:     "web",
			Status:       "success",
		})
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
	secure := true
	if h != nil && h.config != nil && h.config.DevMode {
		secure = false
	}
	if v := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); v != "" {
		lower := strings.ToLower(v)
		if lower == "false" || lower == "0" {
			secure = false
		} else if lower == "true" || lower == "1" {
			secure = true
		}
	}
	return secure
}

// Helper to audit login attempts
func (h *AuthHandler) auditLogin(c *gin.Context, resource, status string) {
	if h.database == nil {
		return
	}

	audit.LogDB(h.database, &db.AuditLog{
		ActionType:   "auth",
		Action:       "login.start",
		ResourceType: "auth",
		Resource:     resource,
		Status:       status,
		IPAddress:    c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
	})
}
