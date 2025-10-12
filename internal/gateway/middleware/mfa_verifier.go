package middleware

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// MFAVerifier is an interface for MFA verification operations used by middleware.
// This allows mocking the MFA service in tests.
type MFAVerifier interface {
	VerifyTOTP(user *db.User, code string, ipAddress string, userAgent string, sessionID *int64) (*mfa.VerificationResult, error)
	VerifyWebAuthnAssertion(user *db.User, sessionJSON []byte, credentialJSON []byte, ipAddress string, userAgent string, sessionID *int64) (*mfa.VerificationResult, error)
}

// Verify that mfa.Service implements MFAVerifier
var _ MFAVerifier = (*mfa.Service)(nil)
