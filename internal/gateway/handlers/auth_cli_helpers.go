package handlers

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

// Helper functions to reduce cognitive complexity in CLI auth handlers

func (h *AuthHandler) cliHandleEnrollmentRedirect(
	c *gin.Context,
	_ string,
	email string,
) {
	if h.sessions == nil {
		return
	}

	// Try to get user for creating web session
	user, err := h.database.GetUser(email)
	if err != nil || user == nil {
		return
	}

	sessionToken, _, createErr := h.sessions.CreateSession(user, auth.SessionMetadata{
		Channel:   "web",
		IPAddress: c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Extra: map[string]interface{}{
			"login_flow": "cli-enrollment",
		},
	})
	if createErr == nil {
		h.setSessionCookie(c, sessionToken)
	} else {
		log.Printf("cli enrollment session create failed: user=%s err=%v", email, createErr)
	}
}

func (h *AuthHandler) cliStampMFAVerification(sessionID int64, mfaTime time.Time) (time.Time, bool) {
	if err := h.sessions.UpdateSessionMFAVerified(sessionID, mfaTime, nil); err != nil {
		log.Printf("failed to stamp session MFA verification: %v", err)
		return mfaTime, false
	}

	if err := h.sessions.UpdateSessionRecentStepUp(sessionID, mfaTime); err != nil {
		log.Printf("failed updating session step-up timestamp: %v", err)
		return mfaTime, false
	}

	return mfaTime, true
}

func buildChallengeURL(challengeRoute string, params url.Values) string {
	if params == nil {
		params = url.Values{}
	}
	params.Set("channel", "cli")
	fullURL := fmt.Sprintf("%s?%s", challengeRoute, params.Encode())
	return fullURL
}
