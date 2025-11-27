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

func (_ *webAuthnUser) WebAuthnIcon() string {
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
