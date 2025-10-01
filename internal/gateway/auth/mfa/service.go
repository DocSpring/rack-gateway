package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/GeertJohan/yubigo"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpPeriodSeconds = 30
	backupCodeCount   = 10
	backupCodeBytes   = 6 // 12 hex chars
)

// yubiAuthVerifier allows mocking of Yubico OTP verification
type yubiAuthVerifier interface {
	Verify(otp string) (*yubigo.YubiResponse, bool, error)
}

// Service orchestrates MFA enrollment, verification, and trusted devices.
type Service struct {
	db               *db.Database
	issuer           string
	trustedDeviceTTL time.Duration
	stepUpWindow     time.Duration
	backupCodePepper []byte
	yubiAuth         yubiAuthVerifier
	webAuthn         *webauthn.WebAuthn
	timeFunc         func() time.Time // for testing
}

// now returns the current time (mockable for tests)
func (s *Service) now() time.Time {
	if s.timeFunc != nil {
		return s.timeFunc()
	}
	return time.Now()
}

// Settings encapsulates runtime MFA configuration loaded from the database.
type Settings struct {
	RequireAllUsers  bool
	TrustedDeviceTTL time.Duration
	StepUpWindow     time.Duration
}

// StartTOTPEnrollmentResult contains the bootstrap payload for the client.
type StartTOTPEnrollmentResult struct {
	MethodID    int64
	Secret      string
	URI         string
	BackupCodes []string
}

// StartYubiOTPEnrollmentResult contains the Yubico OTP enrollment payload.
type StartYubiOTPEnrollmentResult struct {
	MethodID    int64
	BackupCodes []string
}

// StartWebAuthnEnrollmentResult contains the WebAuthn credential creation challenge.
type StartWebAuthnEnrollmentResult struct {
	MethodID         int64
	PublicKeyOptions *protocol.CredentialCreation
	BackupCodes      []string
}

// VerificationResult represents the outcome of an MFA verification.
type VerificationResult struct {
	MethodID        int64
	TrustedDeviceID *int64
}

// NewService creates a new MFA service instance.
func NewService(database *db.Database, issuer string, trustedDeviceTTL, stepUpWindow time.Duration, backupCodePepper []byte, yubiClientID string, yubiSecretKey string, webAuthnRPID string, webAuthnOrigin string) (*Service, error) {
	if len(backupCodePepper) == 0 {
		return nil, fmt.Errorf("backup code pepper is required")
	}
	ttl := trustedDeviceTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	window := stepUpWindow
	if window <= 0 {
		window = 10 * time.Minute
	}

	// Initialize Yubico OTP client (optional)
	var yubiAuth yubiAuthVerifier
	if yubiClientID != "" && yubiSecretKey != "" {
		auth, _ := yubigo.NewYubiAuth(yubiClientID, yubiSecretKey)
		yubiAuth = auth
	}

	// Initialize WebAuthn (optional)
	var webAuthnClient *webauthn.WebAuthn
	if webAuthnRPID != "" && webAuthnOrigin != "" {
		wconfig := &webauthn.Config{
			RPDisplayName: issuer,
			RPID:          webAuthnRPID,
			RPOrigins:     []string{webAuthnOrigin},
		}
		webAuthnClient, _ = webauthn.New(wconfig)
	}

	return &Service{
		db:               database,
		issuer:           issuer,
		trustedDeviceTTL: ttl,
		stepUpWindow:     window,
		backupCodePepper: backupCodePepper,
		yubiAuth:         yubiAuth,
		webAuthn:         webAuthnClient,
	}, nil
}

// StartTOTPEnrollment provisions a TOTP secret and backup codes for the user.
func (s *Service) StartTOTPEnrollment(user *db.User) (*StartTOTPEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if err := s.prepareEnrollment(user.ID); err != nil {
		return nil, err
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.issuer,
		AccountName: user.Email,
		Period:      totpPeriodSeconds,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	method, err := s.db.CreateMFAMethod(user.ID, "totp", "Authenticator App", key.Secret(), nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	backupCodes, err := s.ensureBackupCodes(user.ID)
	if err != nil {
		return nil, err
	}
	// TOTP always generates backup codes on first enrollment
	if backupCodes == nil {
		codes, hashes, err := s.generateBackupCodes()
		if err != nil {
			return nil, err
		}
		if err := s.db.ReplaceBackupCodes(user.ID, hashes); err != nil {
			return nil, err
		}
		backupCodes = codes
	}

	return &StartTOTPEnrollmentResult{
		MethodID:    method.ID,
		Secret:      key.Secret(),
		URI:         key.URL(),
		BackupCodes: backupCodes,
	}, nil
}

// ConfirmTOTP finalizes enrollment by validating the provided code.
func (s *Service) ConfirmTOTP(user *db.User, methodID int64, code string) error {
	if user == nil {
		return fmt.Errorf("user required")
	}
	method, err := s.db.GetMFAMethodByID(methodID)
	if err != nil {
		return err
	}
	if method == nil || method.UserID != user.ID || method.Type != "totp" {
		return fmt.Errorf("invalid MFA method")
	}
	if err := s.validateTOTPCode(method.Secret, user.Email, code); err != nil {
		return err
	}

	return s.finalizeEnrollment(user.ID, method.ID)
}

// VerifyTOTP validates a TOTP or backup code during login or step-up.
// Also supports Yubico OTP if the code looks like a Yubikey OTP.
func (s *Service) VerifyTOTP(user *db.User, code string) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	sanitized := strings.TrimSpace(code)
	if sanitized == "" {
		return nil, fmt.Errorf("code required")
	}

	// Check if it's a Yubico OTP (44 characters)
	if len(sanitized) == 44 && s.yubiAuth != nil {
		result, err := s.VerifyYubiOTP(user, sanitized)
		if err == nil {
			return result, nil
		}
		// Fall through to try TOTP/backup codes
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type != "totp" {
			continue
		}
		if err := s.validateTOTPCode(method.Secret, user.Email, sanitized); err == nil {
			now := time.Now()
			if method.ConfirmedAt == nil {
				if err := s.db.ConfirmMFAMethod(method.ID, now); err != nil {
					return nil, err
				}
			} else {
				if err := s.db.UpdateMFAMethodLastUsed(method.ID, now); err != nil {
					return nil, err
				}
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	// fallback to backup codes
	hash := s.hashBackupCode(sanitized)
	used, err := s.db.MarkBackupCodeUsed(user.ID, hash)
	if err != nil {
		return nil, err
	}
	if used {
		return &VerificationResult{MethodID: 0}, nil
	}

	return nil, fmt.Errorf("invalid code")
}

// StartYubiOTPEnrollment provisions a Yubico OTP method for the user.
// Auto-confirms since we validate the OTP during enrollment.
func (s *Service) StartYubiOTPEnrollment(user *db.User, yubiOTP string) (*StartYubiOTPEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.yubiAuth == nil {
		return nil, fmt.Errorf("yubico OTP not configured")
	}

	yubiOTP = strings.TrimSpace(yubiOTP)
	if len(yubiOTP) < 32 {
		return nil, fmt.Errorf("invalid Yubikey OTP format")
	}

	// Verify the OTP with Yubico servers
	_, ok, err := s.yubiAuth.Verify(yubiOTP)
	if err != nil {
		return nil, fmt.Errorf("failed to verify Yubikey OTP: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("invalid Yubikey OTP")
	}

	// Extract the public ID (first 12 characters)
	publicID := yubiOTP[:12]

	if err := s.prepareEnrollment(user.ID); err != nil {
		return nil, err
	}

	if err := s.checkDuplicateYubikey(user.ID, publicID); err != nil {
		return nil, err
	}

	method, err := s.db.CreateMFAMethod(user.ID, "yubiotp", "Yubikey", publicID, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	// Auto-confirm since we validated the OTP
	if err := s.finalizeEnrollment(user.ID, method.ID); err != nil {
		return nil, err
	}

	backupCodes, err := s.ensureBackupCodes(user.ID)
	if err != nil {
		return nil, err
	}

	return &StartYubiOTPEnrollmentResult{
		MethodID:    method.ID,
		BackupCodes: backupCodes,
	}, nil
}

// VerifyYubiOTP validates a Yubico OTP during login or step-up.
func (s *Service) VerifyYubiOTP(user *db.User, otp string) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.yubiAuth == nil {
		return nil, fmt.Errorf("yubico OTP not configured")
	}

	otp = strings.TrimSpace(otp)
	if len(otp) < 32 {
		return nil, fmt.Errorf("invalid Yubikey OTP format")
	}

	// Verify with Yubico servers
	_, ok, err := s.yubiAuth.Verify(otp)
	if err != nil {
		return nil, fmt.Errorf("failed to verify Yubikey OTP: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("invalid Yubikey OTP")
	}

	// Extract public ID
	publicID := otp[:12]

	// Find matching method
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type == "yubiotp" && method.Secret == publicID {
			now := time.Now()
			if method.ConfirmedAt == nil {
				if err := s.db.ConfirmMFAMethod(method.ID, now); err != nil {
					return nil, err
				}
			} else {
				if err := s.db.UpdateMFAMethodLastUsed(method.ID, now); err != nil {
					return nil, err
				}
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	return nil, fmt.Errorf("yubikey not registered")
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
	return u.user.Name
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	var creds []webauthn.Credential
	for _, method := range u.methods {
		if method.Type != "webauthn" || method.CredentialID == nil || method.PublicKey == nil {
			continue
		}
		var transports []protocol.AuthenticatorTransport
		for _, t := range method.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
		creds = append(creds, webauthn.Credential{
			ID:        method.CredentialID,
			PublicKey: method.PublicKey,
			Transport: transports,
		})
	}
	return creds
}

func (u *webAuthnUser) WebAuthnIcon() string {
	return ""
}

// StartWebAuthnEnrollmentResult contains the challenge and session ID.
type WebAuthnEnrollmentSession struct {
	UserID      int64
	SessionData []byte
}

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
func (s *Service) ConfirmWebAuthnEnrollment(user *db.User, sessionDataJSON []byte, credentialJSON []byte, label string) (int64, error) {
	if user == nil {
		return 0, fmt.Errorf("user required")
	}
	if s.webAuthn == nil {
		return 0, fmt.Errorf("WebAuthn not configured")
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(sessionDataJSON, &session); err != nil {
		return 0, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	// Get methods for user interface
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return 0, err
	}
	waUser := &webAuthnUser{user: user, methods: methods}

	// Parse credential from client
	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(credentialJSON)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse credential: %w", err)
	}

	credential, err := s.webAuthn.CreateCredential(waUser, session, parsedResponse)
	if err != nil {
		return 0, fmt.Errorf("failed to create credential: %w", err)
	}

	// Store credential
	var transports []string
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}

	if label == "" {
		label = "Security Key"
	}

	method, err := s.db.CreateMFAMethod(user.ID, "webauthn", label, "", credential.ID, credential.PublicKey, transports, nil)
	if err != nil {
		return 0, err
	}

	// Finalize enrollment
	if err := s.finalizeEnrollment(user.ID, method.ID); err != nil {
		return 0, err
	}

	// Clean up any pending placeholder methods
	_ = s.db.DeleteUnconfirmedMFAMethods(user.ID)

	return method.ID, nil
}

// VerifyWebAuthn validates a WebAuthn assertion during login or step-up.
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

	waUser := &webAuthnUser{user: user, methods: methods}
	options, session, err := s.webAuthn.BeginLogin(waUser)
	if err != nil {
		return nil, fmt.Errorf("failed to begin login: %w", err)
	}

	_ = options // Will be sent to client

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(credentialJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse assertion: %w", err)
	}

	credential, err := s.webAuthn.ValidateLogin(waUser, *session, parsedResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to validate assertion: %w", err)
	}

	// Find matching method
	for _, method := range methods {
		if method.Type == "webauthn" && string(method.CredentialID) == string(credential.ID) {
			now := time.Now()
			if err := s.db.UpdateMFAMethodLastUsed(method.ID, now); err != nil {
				return nil, err
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	return nil, fmt.Errorf("credential not found")
}

// GenerateBackupCodes replaces the user's backup codes and returns the plaintext set.
func (s *Service) GenerateBackupCodes(userID int64) ([]string, error) {
	codes, hashes, err := s.generateBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.db.ReplaceBackupCodes(userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// TrustedDeviceCookiePayload holds the token and identifier for clients.
type TrustedDeviceCookiePayload struct {
	Token    string
	DeviceID uuid.UUID
	RecordID int64
}

// MintTrustedDevice creates a persistent trusted device record and returns the cookie payload.
func (s *Service) MintTrustedDevice(userID int64, ip, userAgent string) (*TrustedDeviceCookiePayload, error) {
	deviceID := uuid.New()
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate trusted device token: %w", err)
	}
	token := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(tokenBytes)
	tokenHash := hashToken(token)

	expiresAt := time.Now().Add(s.trustedDeviceTTL)
	uaHash := hashUserAgent(userAgent)
	deviceMeta := map[string]interface{}{
		"user_agent": strings.TrimSpace(userAgent),
	}
	device, err := s.db.CreateTrustedDevice(userID, deviceID.String(), tokenHash, expiresAt, ip, uaHash, deviceMeta)
	if err != nil {
		return nil, err
	}
	_ = s.db.TouchTrustedDevice(device.ID, ip)
	payload := &TrustedDeviceCookiePayload{Token: token, DeviceID: deviceID, RecordID: device.ID}
	return payload, nil
}

// ConsumeTrustedDevice validates a trusted-device cookie token and updates its metadata.
func (s *Service) ConsumeTrustedDevice(token string, ip string, userAgent string) (*db.TrustedDevice, error) {
	tokenHash := hashToken(token)
	device, err := s.db.GetTrustedDeviceByHash(tokenHash)
	if err != nil {
		return nil, err
	}
	if device == nil {
		return nil, fmt.Errorf("trusted device not found")
	}
	if device.RevokedAt != nil {
		return nil, fmt.Errorf("trusted device revoked")
	}
	if time.Now().After(device.ExpiresAt) {
		_ = s.db.RevokeTrustedDevice(device.ID, "expired")
		return nil, fmt.Errorf("trusted device expired")
	}
	if device.UserAgentHash != "" && device.UserAgentHash != hashUserAgent(userAgent) {
		return nil, fmt.Errorf("user agent mismatch")
	}
	if err := s.db.TouchTrustedDevice(device.ID, ip); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *Service) generateBackupCodes() ([]string, []string, error) {
	codes := make([]string, 0, backupCodeCount)
	hashes := make([]string, 0, backupCodeCount)
	for i := 0; i < backupCodeCount; i++ {
		buf := make([]byte, backupCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		code := strings.ToUpper(hex.EncodeToString(buf))
		codes = append(codes, code)
		hashes = append(hashes, s.hashBackupCode(code))
	}
	return codes, hashes, nil
}

func (s *Service) hashBackupCode(code string) string {
	mac := hmac.New(sha256.New, s.backupCodePepper)
	mac.Write([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) validateTOTPCode(secret, email, code string) error {
	opts := totp.ValidateOpts{
		Period:    totpPeriodSeconds,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	ok, err := totp.ValidateCustom(code, secret, time.Now(), opts)
	if err != nil {
		return fmt.Errorf("invalid code: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid code")
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func hashUserAgent(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(ua)))
	return hex.EncodeToString(sum[:])
}
