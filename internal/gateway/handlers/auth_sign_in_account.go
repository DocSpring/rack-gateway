package handlers

import (
	"errors"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// errCodeAccountMismatch is the CLI login error for an account that isn't the one bound to the user with its email.
const errCodeAccountMismatch = "account_mismatch"

// bindSignInAccount binds the account signing in (subject is the identity provider's ID for it) to user, or
// refuses it when another account is bound to the user (db.BindUserSignInAccount), so an email address reassigned
// to someone else doesn't inherit the user. A refusal is reported as a failed login. It returns whether the login
// may continue.
func (h *AuthHandler) bindSignInAccount(c *gin.Context, user *db.User, subject, name, channel string) (bool, error) {
	err := h.database.BindUserSignInAccount(user.ID, subject)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, db.ErrSignInAccountMismatch) {
		log.Printf("sign-in account check failed: user=%s err=%v", user.Email, err)
		return false, err
	}
	if h.securityNotifier != nil {
		h.securityNotifier.LoginAttempt(user.Email, name, channel, errCodeAccountMismatch, c.ClientIP(),
			c.GetHeader("User-Agent"), false)
	}
	return false, nil
}

// cliCheckSignInAccount checks the account signing in to a CLI login (bindSignInAccount), and returns the CLI
// error code for a refusal. An email with no user passes: that login is refused as unauthorized later.
func (h *AuthHandler) cliCheckSignInAccount(c *gin.Context, email, name, subject string) string {
	user, err := h.database.GetUser(email)
	if err != nil {
		return "load_failure"
	}
	if user == nil {
		return ""
	}
	ok, err := h.bindSignInAccount(c, user, subject, name, "cli")
	switch {
	case err != nil:
		return "load_failure"
	case !ok:
		return errCodeAccountMismatch
	}
	return ""
}
