package webauthntest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
)

// GenerateRegistration creates a WebAuthn registration response ("none" attestation) for the options returned
// by StartWebAuthnEnrollment. The origin should match the configured WebAuthn origin (e.g. "http://localhost").
// The authenticator data carries mc.Counter and, unless WithoutUserVerification is set, the UV flag.
func (mc *MockCredential) GenerateRegistration(options *protocol.CredentialCreation, origin string) (string, error) {
	if options == nil {
		return "", fmt.Errorf("registration options are required")
	}
	request := options.Response
	if strings.TrimSpace(request.RelyingParty.ID) == "" {
		return "", fmt.Errorf("options missing relying party id")
	}
	if len(request.Challenge) == 0 {
		return "", fmt.Errorf("options missing challenge")
	}

	clientData, err := json.Marshal(map[string]interface{}{
		"type":        "webauthn.create",
		"challenge":   base64.RawURLEncoding.EncodeToString(request.Challenge),
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal client data: %w", err)
	}

	attestationObject, err := cbor.Marshal(map[string]interface{}{
		"fmt":      "none",
		"attStmt":  map[string]interface{}{},
		"authData": mc.registrationAuthData(request.RelyingParty.ID),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal attestation object: %w", err)
	}

	credentialID := base64.RawURLEncoding.EncodeToString(mc.ID)
	response, err := json.Marshal(map[string]interface{}{
		"id":    credentialID,
		"rawId": credentialID,
		"type":  "public-key",
		"response": map[string]interface{}{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestationObject),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal registration: %w", err)
	}
	return string(response), nil
}

// registrationAuthData builds authenticator data with attested credential data: rpIdHash, flags, counter,
// AAGUID (zero), credential ID and the COSE public key.
func (mc *MockCredential) registrationAuthData(rpID string) []byte {
	rpIDHash := sha256.Sum256([]byte(rpID))
	flags := byte(0x45) // user present + user verified + attested credential data
	if mc.WithoutUserVerification {
		flags = 0x41
	}
	data := make([]byte, 0, 37+16+2+len(mc.ID)+len(mc.PublicKey))
	data = append(data, rpIDHash[:]...)
	data = append(data, flags)
	data = binary.BigEndian.AppendUint32(data, mc.Counter)
	data = append(data, make([]byte, 16)...) // AAGUID
	idLen := len(mc.ID)
	if idLen > math.MaxUint16 {
		idLen = math.MaxUint16 // credential IDs are at most 1023 bytes; keep the length field well-formed
	}
	data = binary.BigEndian.AppendUint16(data, uint16(idLen))
	data = append(data, mc.ID...)
	return append(data, mc.PublicKey...)
}
