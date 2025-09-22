package handlers

import (
	"crypto/rand"
	"encoding/base64"
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
	StartWebLogin() string
	CompleteLogin(code, state, codeVerifier string) (*auth.LoginResponse, error)
}

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	oauth    OAuthProvider
	database *db.Database
	config   *config.Config
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(oauth OAuthProvider, database *db.Database, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		oauth:    oauth,
		database: database,
		config:   cfg,
	}
}

// CLILoginStart starts the CLI OAuth flow
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

// CLILoginCallback handles the OAuth redirect callback for CLI
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

// CLILoginComplete completes the CLI OAuth flow
func (h *AuthHandler) CLILoginComplete(c *gin.Context) {
	var req struct {
		State        string `json:"state" binding:"required"`
		CodeVerifier string `json:"code_verifier" binding:"required"`
	}

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

	c.JSON(http.StatusOK, gin.H{
		"token":      resp.Token,
		"email":      resp.Email,
		"name":       resp.Name,
		"expires_at": resp.ExpiresAt,
	})
}

// WebLoginStart starts the web OAuth flow
func (h *AuthHandler) WebLoginStart(c *gin.Context) {
	h.auditLogin(c, "web", "success")
	authURL := h.oauth.StartWebLogin()
	c.Redirect(http.StatusFound, authURL)
}

// WebLoginCallback handles the OAuth callback for web
func (h *AuthHandler) WebLoginCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.String(http.StatusBadRequest, "Missing authorization code or state")
		return
	}

	// Web flow doesn't use PKCE (code_verifier is empty)
	resp, err := h.oauth.CompleteLogin(code, state, "")
	if err != nil {
		c.String(http.StatusInternalServerError, "Authentication failed")
		return
	}

	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"session_token",
		resp.Token,
		30*24*60*60, // 30 days
		"/",
		"",
		secure,
		true,
	)
	c.SetSameSite(http.SameSiteDefaultMode)

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
	c.Redirect(http.StatusFound, "/.gateway/web/rack")
}

// WebLogout handles logout for web sessions
func (h *AuthHandler) WebLogout(c *gin.Context) {
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

// GetCSRFToken returns a CSRF token for web sessions
func (h *AuthHandler) GetCSRFToken(c *gin.Context) {
	// Generate or retrieve CSRF token from session
	token := generateCSRFToken()

	// Set CSRF cookie
	c.SetCookie(
		"csrf_token",
		token,
		60*60, // 1 hour
		"/",
		"",
		true,
		false, // not httpOnly - JS needs to read it
	)

	c.JSON(http.StatusOK, gin.H{"token": token})
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

// generateCSRFToken generates a new CSRF token
func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based token
		return base64.URLEncoding.EncodeToString([]byte(time.Now().String()))
	}
	return base64.URLEncoding.EncodeToString(b)
}
