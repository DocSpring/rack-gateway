package mfa

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// webAuthnChallengeTTL bounds how long a WebAuthn ceremony may take. The challenge is stored
// server-side and deleted when used, so it can't be replayed.
const webAuthnChallengeTTL = 5 * time.Minute

// ErrWebAuthnChallenge is returned when the challenge is unknown, expired, already used or belongs
// to another user or session.
var ErrWebAuthnChallenge = errors.New("WebAuthn challenge expired or already used; please try again")

// StartWebAuthnEnrollment begins WebAuthn credential registration for the given session.
// The registration challenge is stored server-side, bound to the user and session.
func (s *Service) StartWebAuthnEnrollment(user *db.User, sessionID int64) (*StartWebAuthnEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return nil, fmt.Errorf("WebAuthn not configured")
	}

	if err := s.prepareEnrollment(user.ID); err != nil {
		return nil, err
	}

	// Get existing WebAuthn credentials for exclusion
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}

	waUser := &webAuthnUser{user: user, methods: methods}
	options, session, err := s.webAuthn.BeginRegistration(
		waUser,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to begin WebAuthn registration: %w", err)
	}

	if _, err := s.storeChallenge(user.ID, &sessionID, db.WebAuthnChallengeRegistration, session); err != nil {
		return nil, err
	}

	// A pending method gives the enrollment an ID; it has no secret.
	method, err := s.db.CreateMFAMethod(user.ID, "webauthn_pending", "Security Key", "", nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	backupCodes, err := s.backupCodesForEnrollment(user)
	if err != nil {
		return nil, err
	}

	return &StartWebAuthnEnrollmentResult{
		MethodID:         method.ID,
		PublicKeyOptions: options,
		BackupCodes:      backupCodes,
	}, nil
}

// ConfirmWebAuthnEnrollment finalizes WebAuthn registration using the registration challenge stored
// for this session. methodID is the placeholder method ID returned from StartWebAuthnEnrollment.
func (s *Service) ConfirmWebAuthnEnrollment(
	user *db.User,
	sessionID int64,
	methodID int64,
	credentialJSON []byte,
	label string,
) (int64, error) {
	if user == nil {
		return 0, fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return 0, fmt.Errorf("WebAuthn not configured")
	}

	if err := s.validatePendingMethod(user.ID, methodID); err != nil {
		return 0, err
	}

	challenge, err := s.db.ConsumeSessionWebAuthnChallenge(user.ID, sessionID, db.WebAuthnChallengeRegistration)
	if err != nil {
		return 0, challengeError(err)
	}

	credential, err := s.createWebAuthnCredential(user, challenge.SessionData, credentialJSON)
	if err != nil {
		return 0, err
	}

	stored, err := credentialToStore(credential, label)
	if err != nil {
		return 0, err
	}
	if err := s.finalizeEnrollment(user.ID, methodID, stored); err != nil {
		return 0, err
	}

	return methodID, nil
}

// storeChallenge persists go-webauthn session data server-side and returns the opaque challenge ID.
func (s *Service) storeChallenge(
	userID int64,
	sessionID *int64,
	purpose string,
	session *webauthn.SessionData,
) (string, error) {
	session.Expires = s.now().Add(webAuthnChallengeTTL)
	sessionData, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("failed to marshal WebAuthn session: %w", err)
	}
	return s.db.CreateWebAuthnChallenge(userID, sessionID, purpose, sessionData, webAuthnChallengeTTL)
}

func challengeError(err error) error {
	if errors.Is(err, db.ErrWebAuthnChallengeNotFound) {
		return ErrWebAuthnChallenge
	}
	return err
}

func (s *Service) validatePendingMethod(userID, methodID int64) error {
	method, err := s.db.GetMFAMethodByID(methodID)
	if err != nil || method == nil || method.UserID != userID {
		return fmt.Errorf("invalid method ID")
	}
	if method.Type != "webauthn_pending" {
		return fmt.Errorf("method is not pending confirmation")
	}
	return nil
}

func (s *Service) createWebAuthnCredential(
	user *db.User,
	sessionDataJSON []byte,
	credentialJSON []byte,
) (*webauthn.Credential, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionDataJSON, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	waUser := &webAuthnUser{user: user, methods: methods}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(
		strings.NewReader(string(credentialJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credential: %w", err)
	}

	credential, err := s.webAuthn.CreateCredential(waUser, session, parsedResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	return credential, nil
}

// credentialToStore is what enrollment stores of a newly registered credential.
func credentialToStore(credential *webauthn.Credential, label string) (*db.WebAuthnCredential, error) {
	transports := make([]string, 0, len(credential.Transport))
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}

	if label == "" {
		label = "Security Key"
	}

	metadata := map[string]interface{}{"flags": credential.Flags}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return &db.WebAuthnCredential{
		Label:      label,
		ID:         credential.ID,
		PublicKey:  credential.PublicKey,
		Transports: transports,
		Metadata:   metadataJSON,
		SignCount:  credential.Authenticator.SignCount,
	}, nil
}
