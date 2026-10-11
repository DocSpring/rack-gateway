package mfa

import (
	"errors"
	"regexp"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// methodsWithSecret returns the methods of methodType whose stored secret was decrypted. A method whose
// secret couldn't be decrypted is reported and skipped: it can't verify anything, and it must not stop the
// user's other methods (or backup codes) from working.
func methodsWithSecret(methods []*db.MFAMethod, methodType string) []*db.MFAMethod {
	usable := make([]*db.MFAMethod, 0, len(methods))
	for _, method := range methods {
		if method.Type != methodType {
			continue
		}
		if method.SecretErr != nil {
			reportUnusableSecret(method)
			continue
		}
		usable = append(usable, method)
	}
	return usable
}

// reportUnusableSecret logs and reports a method whose stored secret can't be decrypted. The error says
// why (another key, tampered, not encrypted) but never contains the stored value.
func reportUnusableSecret(method *db.MFAMethod) {
	gtwlog.Errorf("mfa: %s method %d of user %d is unusable: %v",
		method.Type, method.ID, method.UserID, method.SecretErr)
	sentry.CaptureException(method.SecretErr)
}

// ErrTOTPUnusable is returned for a TOTP code when every authenticator app the user has is unusable (its secret
// can't be decrypted). No code could verify, so the attempt doesn't count toward the automatic account lock,
// which would also block the user's other factors and backup codes.
var ErrTOTPUnusable = errors.New(
	"your authenticator app can't be used: sign in with a security key or a backup code, then remove the app " +
		"and add it again",
)

// totpCodePattern matches a TOTP code. Backup codes are 12 hex characters, so they never match.
var totpCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

// onlyUnusableTOTP reports whether the user has TOTP methods and none of them can verify a code.
func onlyUnusableTOTP(methods []*db.MFAMethod) bool {
	found := false
	for _, method := range methods {
		if method.Type != "totp" {
			continue
		}
		if method.SecretErr == nil {
			return false
		}
		found = true
	}
	return found
}
