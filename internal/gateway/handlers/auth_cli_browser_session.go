package handlers

import (
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// completeBrowserSessionMFA marks the browser's web session as MFA-verified once the user has
// approved a CLI login with a factor. CLILoginMFAForm signs the browser in with a new web session,
// and without this step that session would stay pending, so every web UI request afterwards would
// demand MFA again.
//
// The factor is proven in the same request that carries the session cookie, so this is the same
// guarantee the web challenge gives. Only a web session belonging to the verified user is touched.
// Failures are logged rather than returned because the CLI login itself is already approved.
func (h *AuthHandler) completeBrowserSessionMFA(c *gin.Context, user *db.User) {
	if h.sessions == nil || user == nil {
		return
	}
	token, err := c.Cookie("session_token")
	if err != nil || strings.TrimSpace(token) == "" {
		return
	}
	result, err := h.sessions.ValidateSession(token, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil || result == nil || result.Session == nil || result.User == nil {
		return
	}
	session := result.Session
	if result.User.ID != user.ID || session.Channel != "web" || session.MFAVerifiedAt != nil {
		return
	}
	if err := h.sessions.UpdateSessionMFAVerified(session.ID, time.Now(), nil); err != nil {
		log.Printf("cli mfa: failed to verify browser session=%d user=%s: %v", session.ID, user.Email, err)
	}
}
