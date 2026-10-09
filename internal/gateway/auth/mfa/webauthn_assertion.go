package mfa

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// attemptContext carries request details recorded with every WebAuthn attempt.
type attemptContext struct {
	userID    int64
	ipAddress string
	userAgent string
	sessionID *int64
}

func (a attemptContext) log(s *Service, methodID *int64, success bool, reason string) {
	_ = s.db.LogWebAuthnAttempt(a.userID, methodID, success, reason, a.ipAddress, a.userAgent, a.sessionID)
}

// StartWebAuthnAssertion begins a WebAuthn assertion (login / step-up) ceremony.
//
// The challenge is stored server-side, bound to the user and (when given) the session that started it,
// and the caller receives only an opaque challenge ID to send back with the assertion. User
// verification (PIN or biometric) is required.
func (s *Service) StartWebAuthnAssertion(
	user *db.User,
	sessionID *int64,
) (*protocol.CredentialAssertion, string, error) {
	if user == nil {
		return nil, "", fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return nil, "", fmt.Errorf("WebAuthn not configured")
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, "", err
	}

	waUser := &webAuthnUser{user: user, methods: methods}
	options, session, err := s.webAuthn.BeginLogin(
		waUser,
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to begin login: %w", err)
	}

	challengeID, err := s.storeChallenge(user.ID, sessionID, db.WebAuthnChallengeAssertion, session)
	if err != nil {
		return nil, "", err
	}
	return options, challengeID, nil
}

// VerifyWebAuthnAssertion validates a WebAuthn assertion against the server-side challenge identified
// by challengeID. The challenge is consumed whether or not the assertion is valid. When the challenge was
// started from a session and sessionID is given, the two must match. Includes rate limiting, automatic
// account locking, and signature-counter clone detection.
func (s *Service) VerifyWebAuthnAssertion(
	user *db.User,
	challengeID []byte,
	credentialJSON []byte,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return nil, fmt.Errorf("WebAuthn not configured")
	}
	attempt := attemptContext{userID: user.ID, ipAddress: ipAddress, userAgent: userAgent, sessionID: sessionID}

	if err := s.ensureUserUnlocked(user.ID); err != nil {
		return nil, err
	}
	if err := s.checkWebAuthnRateLimit(attempt); err != nil {
		return nil, err
	}

	challenge, err := s.db.ConsumeWebAuthnChallenge(
		strings.TrimSpace(string(challengeID)), user.ID, sessionID, db.WebAuthnChallengeAssertion,
	)
	if err != nil {
		attempt.log(s, nil, false, "invalid_challenge")
		return nil, challengeError(err)
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}

	if result, handled, err := s.handleE2EAssertion(attempt, methods); handled {
		return result, err
	}

	credential, err := s.validateAssertion(user, methods, challenge.SessionData, credentialJSON, attempt)
	if err != nil {
		return nil, err
	}

	return s.findAndConfirmCredential(methods, credential, attempt)
}

func (s *Service) checkWebAuthnRateLimit(attempt attemptContext) error {
	return s.enforceAttemptLimit(
		func(id int64, window int) (int, error) {
			return s.db.CountRecentWebAuthnAttempts(id, window)
		},
		attempt.userID,
		5,
		func() error {
			attempt.log(s, nil, false, "rate_limited")
			return nil
		},
	)
}

func (s *Service) handleE2EAssertion(
	attempt attemptContext,
	methods []*db.MFAMethod,
) (*VerificationResult, bool, error) {
	result, handled, err := s.maybeHandleE2EWebAuthn(methods, func(method *db.MFAMethod) error {
		attempt.log(s, &method.ID, true, "e2e_test")
		return nil
	})
	if !handled {
		return nil, false, nil
	}
	if err != nil {
		attempt.log(s, nil, false, "no_method_enrolled")
	}
	return result, true, err
}

func (s *Service) validateAssertion(
	user *db.User,
	methods []*db.MFAMethod,
	sessionJSON []byte,
	credentialJSON []byte,
	attempt attemptContext,
) (*webauthn.Credential, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionJSON, &session); err != nil {
		attempt.log(s, nil, false, "invalid_session")
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(
		strings.NewReader(string(credentialJSON)),
	)
	if err != nil {
		attempt.log(s, nil, false, "invalid_credential")
		return nil, fmt.Errorf("failed to parse assertion: %w", err)
	}

	waUser := &webAuthnUser{user: user, methods: methods}
	credential, err := s.webAuthn.ValidateLogin(waUser, session, parsedResponse)
	if err != nil {
		attempt.log(s, nil, false, "validation_failed")
		if lockErr := s.checkAndLockAccount(user.ID); lockErr != nil {
			return nil, lockErr
		}
		return nil, fmt.Errorf("failed to validate assertion: %w", err)
	}

	return credential, nil
}

// findAndConfirmCredential matches the validated credential to its MFA method, rejects a signature
// counter that didn't increase (possible cloned authenticator), and stores the new counter.
func (s *Service) findAndConfirmCredential(
	methods []*db.MFAMethod,
	credential *webauthn.Credential,
	attempt attemptContext,
) (*VerificationResult, error) {
	method := findWebAuthnMethod(methods, credential.ID)
	if method == nil {
		attempt.log(s, nil, false, "credential_not_found")
		if err := s.checkAndLockAccount(attempt.userID); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("credential not found")
	}

	if credential.Authenticator.CloneWarning {
		attempt.log(s, &method.ID, false, "sign_count_not_increased")
		if err := s.checkAndLockAccount(attempt.userID); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("security key signature counter did not increase; the key may have been cloned")
	}

	if err := s.db.UpdateMFAMethodSignCount(method.ID, credential.Authenticator.SignCount); err != nil {
		return nil, err
	}
	if err := s.touchMFAMethod(method); err != nil {
		return nil, err
	}
	attempt.log(s, &method.ID, true, "")
	return &VerificationResult{MethodID: method.ID}, nil
}

func findWebAuthnMethod(methods []*db.MFAMethod, credentialID []byte) *db.MFAMethod {
	for _, method := range methods {
		if method.Type == "webauthn" && string(method.CredentialID) == string(credentialID) {
			return method
		}
	}
	return nil
}
