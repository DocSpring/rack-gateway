package webauthntest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
)

// MockCredential represents a mock WebAuthn credential for testing
type MockCredential struct {
	ID         []byte
	PublicKey  []byte
	PrivateKey *ecdsa.PrivateKey
}

// GenerateMockCredential creates a mock WebAuthn credential with a valid key pair
func GenerateMockCredential() (*MockCredential, error) {
	// Generate ECDSA P-256 key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate random credential ID
	credID := make([]byte, 32)
	if _, err := rand.Read(credID); err != nil {
		return nil, fmt.Errorf("failed to generate credential ID: %w", err)
	}

	// Marshal public key in COSE format
	publicKey, err := marshalPublicKeyCOSE(privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	return &MockCredential{
		ID:         credID,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}, nil
}

// GenerateAssertionForSession creates a valid WebAuthn assertion response for the
// provided session payload returned by StartWebAuthnAssertion. The origin should
// match the configured WebAuthn origin (e.g., "http://localhost").
func (mc *MockCredential) GenerateAssertionForSession(sessionJSON []byte, origin string) (string, error) {
	if len(sessionJSON) == 0 {
		return "", fmt.Errorf("session data is required")
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(sessionJSON, &session); err != nil {
		return "", fmt.Errorf("failed to unmarshal session: %w", err)
	}

	if strings.TrimSpace(session.RelyingPartyID) == "" {
		return "", fmt.Errorf("session missing relying party id")
	}

	challenge, err := base64.RawURLEncoding.DecodeString(session.Challenge)
	if err != nil {
		return "", fmt.Errorf("failed to decode challenge: %w", err)
	}

	credentialID := mc.ID
	if len(session.AllowedCredentialIDs) > 0 {
		credentialID = session.AllowedCredentialIDs[0]
	}
	if len(credentialID) == 0 {
		return "", fmt.Errorf("session missing credential id")
	}
	if len(mc.ID) > 0 && !bytes.Equal(mc.ID, credentialID) {
		return "", fmt.Errorf("mock credential does not match session credential")
	}

	if strings.TrimSpace(origin) == "" {
		origin = fmt.Sprintf("http://%s", session.RelyingPartyID)
	}

	assertion, err := mc.buildAssertion(challenge, session.Challenge, session.RelyingPartyID, credentialID, origin, session.UserID)
	if err != nil {
		return "", err
	}

	assertionBytes, err := json.Marshal(assertion)
	if err != nil {
		return "", fmt.Errorf("failed to marshal assertion: %w", err)
	}

	return string(assertionBytes), nil
}

func (mc *MockCredential) buildAssertion(challenge []byte, challengeEncoded string, rpID string, credentialID []byte, origin string, userHandle []byte) (map[string]interface{}, error) {
	rpIDHash := sha256.Sum256([]byte(rpID))
	authData := make([]byte, 37)
	copy(authData[0:32], rpIDHash[:])
	authData[32] = 0x05 // user present + user verified

	clientDataJSON := map[string]interface{}{
		"type":        "webauthn.get",
		"challenge":   challengeEncoded,
		"origin":      origin,
		"crossOrigin": false,
	}

	clientDataBytes, err := json.Marshal(clientDataJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal client data: %w", err)
	}

	clientDataHash := sha256.Sum256(clientDataBytes)
	signaturePayload := make([]byte, len(authData)+len(clientDataHash))
	copy(signaturePayload, authData)
	copy(signaturePayload[len(authData):], clientDataHash[:])

	signature, err := mc.sign(signaturePayload)
	if err != nil {
		return nil, fmt.Errorf("failed to sign assertion: %w", err)
	}

	response := map[string]interface{}{
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataBytes),
		"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
		"signature":         base64.RawURLEncoding.EncodeToString(signature),
	}
	if len(userHandle) > 0 {
		response["userHandle"] = base64.RawURLEncoding.EncodeToString(userHandle)
	}

	credentialB64 := base64.RawURLEncoding.EncodeToString(credentialID)

	return map[string]interface{}{
		"id":       credentialB64,
		"rawId":    credentialB64,
		"type":     "public-key",
		"response": response,
	}, nil
}

// sign creates an ECDSA signature over the data
func (mc *MockCredential) sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)

	r, s, err := ecdsa.Sign(rand.Reader, mc.PrivateKey, hash[:])
	if err != nil {
		return nil, err
	}

	// Encode as ASN.1 DER format (required by WebAuthn)
	return asn1.Marshal(struct {
		R, S *big.Int
	}{R: r, S: s})
}

// marshalPublicKeyCOSE marshals an ECDSA public key in COSE format
func marshalPublicKeyCOSE(pubKey ecdsa.PublicKey) ([]byte, error) {
	// COSE key format for ES256 (ECDSA P-256)
	coseKey := map[int]interface{}{
		1:  2,                               // kty: EC2
		3:  webauthncose.AlgES256,           // alg: ES256 (-7)
		-1: int(webauthncose.P256),          // crv: P-256
		-2: padCoordinate(pubKey.X.Bytes()), // x coordinate (32 bytes)
		-3: padCoordinate(pubKey.Y.Bytes()), // y coordinate (32 bytes)
	}

	return cbor.Marshal(coseKey)
}

func padCoordinate(input []byte) []byte {
	const coordinateSize = 32
	if len(input) >= coordinateSize {
		return input
	}
	buf := make([]byte, coordinateSize)
	copy(buf[coordinateSize-len(input):], input)
	return buf
}
