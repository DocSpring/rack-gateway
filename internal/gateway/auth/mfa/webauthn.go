package mfa

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// StartWebAuthnEnrollment begins WebAuthn credential registration.
// Returns the challenge options and a session identifier for the frontend.
func (s *Service) StartWebAuthnEnrollment(user *db.User) (*StartWebAuthnEnrollmentResult, string, error) {
	if user == nil {
		return nil, "", fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return nil, "", fmt.Errorf("WebAuthn not configured")
	}

	if err := s.prepareEnrollment(user.ID); err != nil {
		return nil, "", err
	}

	// Get existing WebAuthn credentials for exclusion
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, "", err
	}

	waUser := &webAuthnUser{user: user, methods: methods}
	options, session, err := s.webAuthn.BeginRegistration(waUser)
	if err != nil {
		return nil, "", fmt.Errorf("failed to begin WebAuthn registration: %w", err)
	}

	// Generate a unique session ID for this enrollment
	sessionID := fmt.Sprintf("webauthn_enroll_%d_%d", user.ID, s.now().UnixNano())

	// Store session data - caller must persist this (typically in user session metadata)
	sessionData, err := json.Marshal(session)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal session: %w", err)
	}

	// Store a minimal placeholder method to get an ID
	method, err := s.db.CreateMFAMethod(user.ID, "webauthn_pending", "Security Key", sessionID, nil, nil, nil, nil)
	if err != nil {
		return nil, "", err
	}

	backupCodes, err := s.ensureBackupCodes(user.ID)
	if err != nil {
		return nil, "", err
	}

	result := &StartWebAuthnEnrollmentResult{
		MethodID:         method.ID,
		PublicKeyOptions: options,
		BackupCodes:      backupCodes,
	}

	// Return session data so caller can store it in their session
	return result, string(sessionData), nil
}

// ConfirmWebAuthnEnrollment finalizes WebAuthn registration.
// sessionDataJSON is the WebAuthn session data returned from StartWebAuthnEnrollment.
// methodID is the placeholder method ID returned from StartWebAuthnEnrollment.
func (s *Service) ConfirmWebAuthnEnrollment(
	user *db.User,
	methodID int64,
	sessionDataJSON []byte,
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

	credential, err := s.createWebAuthnCredential(user, sessionDataJSON, credentialJSON)
	if err != nil {
		return 0, err
	}

	if err := s.storeParsedCredential(methodID, credential, label); err != nil {
		return 0, err
	}

	if err := s.finalizeEnrollment(user.ID, methodID); err != nil {
		return 0, err
	}

	return methodID, nil
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

func (s *Service) storeParsedCredential(
	methodID int64,
	credential *webauthn.Credential,
	label string,
) error {
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
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return s.db.UpdateMFAMethodCredential(
		methodID,
		"webauthn",
		label,
		credential.ID,
		credential.PublicKey,
		transports,
		metadataJSON,
	)
}

// StartWebAuthnAssertion begins a WebAuthn assertion (login) ceremony.
// Returns the challenge options and session data that must be stored for verification.
func (s *Service) StartWebAuthnAssertion(user *db.User) (*protocol.CredentialAssertion, []byte, error) {
	if user == nil {
		return nil, nil, fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return nil, nil, fmt.Errorf("WebAuthn not configured")
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, nil, err
	}

	waUser := &webAuthnUser{user: user, methods: methods}

	options, session, err := s.webAuthn.BeginLogin(waUser)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin login: %w", err)
	}

	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal session: %w", err)
	}

	return options, sessionJSON, nil
}

// VerifyWebAuthnAssertion validates a WebAuthn assertion response using stored session data.
// Includes rate limiting and automatic account locking.
func (s *Service) VerifyWebAuthnAssertion(
	user *db.User,
	sessionJSON []byte,
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

	if err := s.ensureUserUnlocked(user.ID); err != nil {
		return nil, err
	}

	if err := s.checkWebAuthnRateLimit(user.ID, ipAddress, userAgent, sessionID); err != nil {
		return nil, err
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}

	if result, handled, err := s.handleE2EAssertion(
		user.ID,
		methods,
		ipAddress,
		userAgent,
		sessionID,
	); handled {
		return result, err
	}

	credential, err := s.validateAssertion(
		user,
		methods,
		sessionJSON,
		credentialJSON,
		ipAddress,
		userAgent,
		sessionID,
	)
	if err != nil {
		return nil, err
	}

	return s.findAndConfirmCredential(user.ID, methods, credential, ipAddress, userAgent, sessionID)
}

func (s *Service) checkWebAuthnRateLimit(
	userID int64,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) error {
	return s.enforceAttemptLimit(
		func(id int64, window int) (int, error) {
			return s.db.CountRecentWebAuthnAttempts(id, window)
		},
		userID,
		5,
		func() error {
			_ = s.db.LogWebAuthnAttempt(userID, nil, false, "rate_limited", ipAddress, userAgent, sessionID)
			return nil
		},
	)
}

func (s *Service) handleE2EAssertion(
	userID int64,
	methods []*db.MFAMethod,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (*VerificationResult, bool, error) {
	result, handled, err := s.maybeHandleE2EWebAuthn(methods, func(method *db.MFAMethod) error {
		_ = s.db.LogWebAuthnAttempt(userID, &method.ID, true, "e2e_test", ipAddress, userAgent, sessionID)
		return nil
	})
	if !handled {
		return nil, false, nil
	}
	if err != nil {
		_ = s.db.LogWebAuthnAttempt(userID, nil, false, "no_method_enrolled", ipAddress, userAgent, sessionID)
	}
	return result, true, err
}

func (s *Service) validateAssertion(
	user *db.User,
	methods []*db.MFAMethod,
	sessionJSON []byte,
	credentialJSON []byte,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) ([]byte, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionJSON, &session); err != nil {
		_ = s.db.LogWebAuthnAttempt(user.ID, nil, false, "invalid_session", ipAddress, userAgent, sessionID)
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	waUser := &webAuthnUser{user: user, methods: methods}

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(
		strings.NewReader(string(credentialJSON)),
	)
	if err != nil {
		_ = s.db.LogWebAuthnAttempt(user.ID, nil, false, "invalid_credential", ipAddress, userAgent, sessionID)
		return nil, fmt.Errorf("failed to parse assertion: %w", err)
	}

	credential, err := s.webAuthn.ValidateLogin(waUser, session, parsedResponse)
	if err != nil {
		_ = s.db.LogWebAuthnAttempt(user.ID, nil, false, "validation_failed", ipAddress, userAgent, sessionID)
		if lockErr := s.checkAndLockAccount(user.ID); lockErr != nil {
			return nil, lockErr
		}
		return nil, fmt.Errorf("failed to validate assertion: %w", err)
	}

	return credential.ID, nil
}

func (s *Service) findAndConfirmCredential(
	userID int64,
	methods []*db.MFAMethod,
	credentialID []byte,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (*VerificationResult, error) {
	for _, method := range methods {
		if method.Type == "webauthn" && string(method.CredentialID) == string(credentialID) {
			if err := s.touchMFAMethod(method); err != nil {
				return nil, err
			}
			_ = s.db.LogWebAuthnAttempt(userID, &method.ID, true, "", ipAddress, userAgent, sessionID)
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	_ = s.db.LogWebAuthnAttempt(userID, nil, false, "credential_not_found", ipAddress, userAgent, sessionID)
	if err := s.checkAndLockAccount(userID); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("credential not found")
}

// VerifyWebAuthn validates a WebAuthn assertion during login or step-up (legacy, for web UI).
// Deprecated: Use StartWebAuthnAssertion + VerifyWebAuthnAssertion for better session management.
func (s *Service) VerifyWebAuthn(user *db.User, credentialJSON []byte) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return nil, fmt.Errorf("WebAuthn not configured")
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}

	if result, handled, err := s.maybeHandleE2EWebAuthn(methods, nil); handled {
		return result, err
	}

	credentialID, err := s.performLegacyWebAuthnValidation(user, methods, credentialJSON)
	if err != nil {
		return nil, err
	}

	return s.matchCredentialToMethod(methods, credentialID)
}

func (s *Service) performLegacyWebAuthnValidation(
	user *db.User,
	methods []*db.MFAMethod,
	credentialJSON []byte,
) ([]byte, error) {
	waUser := &webAuthnUser{user: user, methods: methods}
	options, session, err := s.webAuthn.BeginLogin(waUser)
	if err != nil {
		return nil, fmt.Errorf("failed to begin login: %w", err)
	}

	_ = options

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(
		strings.NewReader(string(credentialJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse assertion: %w", err)
	}

	credential, err := s.webAuthn.ValidateLogin(waUser, *session, parsedResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to validate assertion: %w", err)
	}

	return credential.ID, nil
}

func (s *Service) matchCredentialToMethod(
	methods []*db.MFAMethod,
	credentialID []byte,
) (*VerificationResult, error) {
	for _, method := range methods {
		if method.Type == "webauthn" && string(method.CredentialID) == string(credentialID) {
			if err := s.touchMFAMethod(method); err != nil {
				return nil, err
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}
	return nil, fmt.Errorf("credential not found")
}

// webAuthnUser wraps db.User to implement webauthn.User interface
type webAuthnUser struct {
	user    *db.User
	methods []*db.MFAMethod
}

func (u *webAuthnUser) WebAuthnID() []byte {
	return []byte(fmt.Sprintf("%d", u.user.ID))
}

func (u *webAuthnUser) WebAuthnName() string {
	return u.user.Email
}

func (u *webAuthnUser) WebAuthnDisplayName() string {
	if strings.TrimSpace(u.user.Name) != "" {
		return u.user.Name
	}
	return u.user.Email
}

func (u *webAuthnUser) WebAuthnIcon() string {
	return ""
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	creds := make([]webauthn.Credential, 0, len(u.methods))
	for _, method := range u.methods {
		if method.Type != "webauthn" {
			continue
		}

		creds = append(creds, webauthn.Credential{
			ID:              method.CredentialID,
			PublicKey:       method.PublicKey,
			AttestationType: "",
			Transport:       convertTransports(method.Transports),
			Flags:           extractCredentialFlags(method.Metadata),
		})
	}
	return creds
}

func extractCredentialFlags(metadata []byte) webauthn.CredentialFlags {
	var flags webauthn.CredentialFlags
	if len(metadata) == 0 {
		return flags
	}

	var metadataMap map[string]interface{}
	if err := json.Unmarshal(metadata, &metadataMap); err != nil {
		return flags
	}

	flagsData, ok := metadataMap["flags"]
	if !ok {
		return flags
	}

	flagsMap, ok := flagsData.(map[string]interface{})
	if !ok {
		return flags
	}

	return webauthn.CredentialFlags{
		UserPresent:    boolValue(flagsMap["userPresent"]),
		UserVerified:   boolValue(flagsMap["userVerified"]),
		BackupEligible: boolValue(flagsMap["backupEligible"]),
		BackupState:    boolValue(flagsMap["backupState"]),
	}
}

func boolValue(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func (s *Service) maybeHandleE2EWebAuthn(
	methods []*db.MFAMethod,
	onSuccess func(*db.MFAMethod) error,
) (*VerificationResult, bool, error) {
	if os.Getenv("E2E_TEST_MODE") != "true" {
		return nil, false, nil
	}
	for _, method := range methods {
		if method.Type != "webauthn" {
			continue
		}
		if err := s.touchMFAMethod(method); err != nil {
			return nil, true, err
		}
		if onSuccess != nil {
			if err := onSuccess(method); err != nil {
				return nil, true, err
			}
		}
		return &VerificationResult{MethodID: method.ID}, true, nil
	}
	return nil, true, fmt.Errorf("no WebAuthn method available")
}

func convertTransports(values []string) []protocol.AuthenticatorTransport {
	transports := make([]protocol.AuthenticatorTransport, 0, len(values))
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "usb":
			transports = append(transports, protocol.USB)
		case "nfc":
			transports = append(transports, protocol.NFC)
		case "ble":
			transports = append(transports, protocol.BLE)
		case "internal":
			transports = append(transports, protocol.Internal)
		default:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				transports = append(transports, protocol.AuthenticatorTransport(trimmed))
			}
		}
	}
	return transports
}
