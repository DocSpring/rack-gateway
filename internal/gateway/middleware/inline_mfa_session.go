package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// inlineMFASessionKey holds the session change an inline MFA proof verified on this request earned.
const inlineMFASessionKey = "inline_mfa_session_change"

var errNoInlineMFASession = errors.New("no session to save the inline MFA verification to")

// inlineMFASessionChange is what an inline MFA proof verified on this request changes in its session: it opens the
// step-up window and, for a session that still owed its MFA challenge, completes the challenge. The MFA checks apply
// it to the request's session at once, but it's saved only once the request's audit attempt is written
// (AuditBeforeActing): nothing changes before its audit entry exists, so a request the audit log refuses leaves the
// session as it was.
type inlineMFASessionChange struct {
	at                 time.Time
	completesChallenge bool
}

// applyInlineMFA records that the request's inline MFA proof was verified: from here on the request's session has
// stepped up (and completed its MFA challenge, with completesChallenge), and AuditBeforeActing saves that.
func applyInlineMFA(c *gin.Context, session *db.UserSession, completesChallenge bool) {
	now := time.Now()
	session.RecentStepUpAt = &now
	if completesChallenge {
		session.MFAVerifiedAt = &now
	}
	c.Set(inlineMFASessionKey, inlineMFASessionChange{at: now, completesChallenge: completesChallenge})
}

// inlineMFAVerified reports whether the request's inline MFA proof has been verified, so later MFA checks in the
// same request don't verify it a second time (which would trip TOTP replay protection).
func inlineMFAVerified(c *gin.Context) bool {
	_, verified := c.Get(inlineMFASessionKey)
	return verified
}

// saveInlineMFA saves the session change the request's inline MFA proof earned, once its audit attempt is written.
// It answers 500 when the change can't be saved.
func saveInlineMFA(c *gin.Context, database *db.Database, session *db.UserSession) bool {
	value, ok := c.Get(inlineMFASessionKey)
	if !ok {
		return true
	}
	change, ok := value.(inlineMFASessionChange)
	var err error
	switch {
	case !ok || database == nil || session == nil:
		err = errNoInlineMFASession
	case change.completesChallenge:
		err = database.UpdateSessionMFAVerified(session.ID, change.at, session.TrustedDeviceID)
	default:
		err = database.UpdateSessionRecentStepUp(session.ID, change.at)
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":   "mfa_verification_record_failed",
			"message": "Failed to record MFA verification. Please try again.",
		})
		return false
	}
	return true
}
