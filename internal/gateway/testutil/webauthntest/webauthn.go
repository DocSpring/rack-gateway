package webauthntest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// MockCredential represents a mock WebAuthn credential for testing
type MockCredential struct {
	ID         []byte
	PublicKey  []byte
	PrivateKey *ecdsa.PrivateKey
	// Counter is the signature counter reported in generated assertions.
	Counter uint32
	// WithoutUserVerification clears the UV flag (e.g. a key used without its PIN).
	WithoutUserVerification bool
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

// GenerateAssertion creates a valid WebAuthn assertion response for the options returned by
// StartWebAuthnAssertion. The origin should match the configured WebAuthn origin (e.g. "http://localhost").
// The signature counter in the authenticator data is mc.Counter.
func (mc *MockCredential) GenerateAssertion(options *protocol.CredentialAssertion, origin string) (string, error) {
	if options == nil {
		return "", fmt.Errorf("assertion options are required")
	}
	request := options.Response
	if strings.TrimSpace(request.RelyingPartyID) == "" {
		return "", fmt.Errorf("options missing relying party id")
	}
	if len(request.Challenge) == 0 {
		return "", fmt.Errorf("options missing challenge")
	}

	credentialID := mc.ID
	if len(request.AllowedCredentials) > 0 {
		credentialID = request.AllowedCredentials[0].CredentialID
	}
	if len(credentialID) == 0 {
		return "", fmt.Errorf("options missing credential id")
	}
	if len(mc.ID) > 0 && !bytes.Equal(mc.ID, credentialID) {
		return "", fmt.Errorf("mock credential does not match allowed credential")
	}

	if strings.TrimSpace(origin) == "" {
		origin = fmt.Sprintf("http://%s", request.RelyingPartyID)
	}

	challenge := base64.RawURLEncoding.EncodeToString(request.Challenge)
	assertion, err := mc.buildAssertion(challenge, request.RelyingPartyID, credentialID, origin, nil)
	if err != nil {
		return "", err
	}

	assertionBytes, err := json.Marshal(assertion)
	if err != nil {
		return "", fmt.Errorf("failed to marshal assertion: %w", err)
	}

	return string(assertionBytes), nil
}

// GenerateAssertionFromOptionsJSON is GenerateAssertion for the JSON "options" object returned by the
// /auth/mfa/webauthn/assertion/start endpoint.
func (mc *MockCredential) GenerateAssertionFromOptionsJSON(optionsJSON []byte, origin string) (string, error) {
	var options protocol.CredentialAssertion
	if err := json.Unmarshal(optionsJSON, &options); err != nil {
		return "", fmt.Errorf("failed to unmarshal assertion options: %w", err)
	}
	return mc.GenerateAssertion(&options, origin)
}

func (mc *MockCredential) buildAssertion(
	challengeEncoded string,
	rpID string,
	credentialID []byte,
	origin string,
	userHandle []byte,
) (map[string]interface{}, error) {
	rpIDHash := sha256.Sum256([]byte(rpID))
	authData := make([]byte, 37)
	copy(authData[0:32], rpIDHash[:])
	authData[32] = 0x05 // user present + user verified
	if mc.WithoutUserVerification {
		authData[32] = 0x01 // user present only
	}
	binary.BigEndian.PutUint32(authData[33:37], mc.Counter)

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
	ecdhKey, err := pubKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("failed to convert public key to ECDH: %w", err)
	}

	uncompressed := ecdhKey.Bytes()
	if len(uncompressed) != 65 || uncompressed[0] != 0x04 {
		return nil, fmt.Errorf("invalid uncompressed public key format")
	}

	// Extract X and Y coordinates from uncompressed format
	// Format: 0x04 || X (32 bytes) || Y (32 bytes)
	xCoord := uncompressed[1:33]
	yCoord := uncompressed[33:65]

	// COSE key format for ES256 (ECDSA P-256)
	coseKey := map[int]interface{}{
		1:  2,                      // kty: EC2
		3:  webauthncose.AlgES256,  // alg: ES256 (-7)
		-1: int(webauthncose.P256), // crv: P-256
		-2: padCoordinate(xCoord),  // x coordinate (32 bytes)
		-3: padCoordinate(yCoord),  // y coordinate (32 bytes)
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
