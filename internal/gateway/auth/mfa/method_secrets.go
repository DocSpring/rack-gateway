package mfa

import (
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
