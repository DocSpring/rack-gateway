package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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

// cliRedirectWithError redirects to the MFA challenge route with an error parameter
func cliRedirectWithError(c *gin.Context, errorCode string) {
	challengeRoute := WebRoute("auth/mfa/challenge")
	params := url.Values{}
	params.Set("error", errorCode)
	c.Redirect(http.StatusTemporaryRedirect, buildChallengeURL(challengeRoute, params))
}

// cliExchangeOAuthCode exchanges the OAuth code for user profile information
func (h *AuthHandler) cliExchangeOAuthCode(
	c *gin.Context,
	record *db.CLILoginState,
	state string,
) (string, bool) {
	if !record.Code.Valid || !record.CodeVerifier.Valid {
		cliRedirectWithError(c, "session_incomplete")
		return "", false
	}

	loginResp, err := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
	if err != nil {
		cliRedirectWithError(c, "exchange_failed")
		return "", false
	}

	if err := h.database.SetCLILoginProfile(state, loginResp.Email, loginResp.Name); err != nil {
		cliRedirectWithError(c, "persist_failure")
		return "", false
	}

	return strings.TrimSpace(loginResp.Email), true
}

// cliLoadAndValidateUser loads and validates user for CLI MFA flow
func (h *AuthHandler) cliLoadAndValidateUser(
	c *gin.Context,
	state string,
	loginEmail string,
) (*db.User, bool) {
	if loginEmail == "" {
		errMsg := "Unable to determine account information for CLI login."
		if err := h.database.FailCLILoginState(state, errMsg); err != nil {
			log.Printf("cli login fail (missing email): state=%s err=%v", state, err)
		}
		cliRedirectWithError(c, "unauthorized")
		return nil, false
	}

	userRecord, err := h.database.GetUser(loginEmail)
	if err != nil {
		cliRedirectWithError(c, "load_failure")
		return nil, false
	}

	if userRecord == nil {
		if err := h.database.FailCLILoginState(state, "User not authorized for this gateway."); err != nil {
			log.Printf("cli login fail (unknown user): state=%s err=%v", state, err)
		}
		cliRedirectWithError(c, "unauthorized")
		return nil, false
	}

	return userRecord, true
}

// cliHandleNoMFARequired handles the case where MFA is not required for the user
func (h *AuthHandler) cliHandleNoMFARequired(c *gin.Context, state string) bool {
	if err := h.database.MarkCLILoginVerified(state, nil); err != nil {
		log.Printf("cli login mark verified failed: state=%s err=%v", state, err)
		cliRedirectWithError(c, "persist_failure")
		return false
	}
	c.Redirect(http.StatusTemporaryRedirect, WebRoute("cli/auth/success"))
	return true
}

// cliHandleEnrollmentRequired handles the enrollment redirect for unenrolled users
func (h *AuthHandler) cliHandleEnrollmentRequired(c *gin.Context, state string, email string) {
	if err := h.database.FailCLILoginState(state, cliEnrollmentErrorMessage); err != nil {
		log.Printf("cli login fail (enrollment required): state=%s err=%v", state, err)
	}

	h.cliHandleEnrollmentRedirect(c, state, email)

	enrollParams := url.Values{}
	enrollParams.Set("enrollment", "required")
	enrollParams.Set("channel", "cli")
	enrollParams.Set("state", state)
	c.Redirect(
		http.StatusTemporaryRedirect,
		fmt.Sprintf("%s?%s", WebRoute("account/security"), enrollParams.Encode()),
	)
}

// cliExchangeIfNeeded exchanges OAuth code if login email is not yet available
func (h *AuthHandler) cliExchangeIfNeeded(
	c *gin.Context,
	record *db.CLILoginState,
	state string,
) (*db.CLILoginState, bool) {
	if record.LoginEmail.Valid {
		return record, true
	}

	if !record.Code.Valid || !record.CodeVerifier.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_incomplete"})
		return nil, false
	}

	loginResp, err := h.oauth.CompleteLogin(record.Code.String, state, record.CodeVerifier.String)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "exchange_failed"})
		return nil, false
	}

	if err := h.database.SetCLILoginProfile(state, loginResp.Email, loginResp.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist_failure"})
		return nil, false
	}

	record.LoginEmail.String = loginResp.Email
	record.LoginEmail.Valid = true
	return record, true
}

// cliGetUserRecord loads user record from database with JSON error handling
func (h *AuthHandler) cliGetUserRecord(c *gin.Context, email string) (*db.User, bool) {
	userRecord, err := h.database.GetUser(strings.TrimSpace(email))
	if err != nil || userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return userRecord, true
}
