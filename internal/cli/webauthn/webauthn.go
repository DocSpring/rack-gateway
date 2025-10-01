package webauthn

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/go-ctap/ctaphid/pkg/ctaptypes"
	"github.com/go-ctap/ctaphid/pkg/sugar"
	"github.com/go-ctap/ctaphid/pkg/webauthntypes"
)

// AssertionOptions contains the challenge and credential info from the server
type AssertionOptions struct {
	Challenge        string   `json:"challenge"`
	RPID             string   `json:"rp_id"`
	AllowCredentials []string `json:"allow_credentials"`
	Timeout          int      `json:"timeout"`
	UserVerification string   `json:"user_verification"`
	Origin           string   `json:"origin"` // Gateway URL origin for clientData
}

// AssertionResponse contains the signed assertion to send back to the server
type AssertionResponse struct {
	CredentialID      string `json:"credential_id"`
	AuthenticatorData string `json:"authenticator_data"`
	ClientDataJSON    string `json:"client_data_json"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"user_handle"`
}

// GetAssertion prompts the user to authenticate with their FIDO2 device
func GetAssertion(options AssertionOptions) (*AssertionResponse, error) {
	// Select FIDO2 device
	dev, err := sugar.SelectDevice()
	if err != nil {
		return nil, fmt.Errorf("failed to select FIDO2 device: %w", err)
	}
	defer func() {
		_ = dev.Close() // Ignore close errors as we're done with the device
	}()

	// Build client data JSON
	origin := options.Origin
	if origin == "" {
		origin = "http://localhost" // Fallback for testing
	}
	clientData := map[string]interface{}{
		"type":      "webauthn.get",
		"challenge": options.Challenge,
		"origin":    origin,
	}
	clientDataJSON, err := json.Marshal(clientData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal client data: %w", err)
	}

	// Hash the client data
	h := sha256.New()
	h.Write(clientDataJSON)
	clientDataHash := h.Sum(nil)

	// Decode allowed credentials
	var allowList []webauthntypes.PublicKeyCredentialDescriptor
	for _, credID := range options.AllowCredentials {
		credBytes, err := base64.RawURLEncoding.DecodeString(credID)
		if err != nil {
			return nil, fmt.Errorf("failed to decode credential ID: %w", err)
		}
		allowList = append(allowList, webauthntypes.PublicKeyCredentialDescriptor{
			Type: "public-key",
			ID:   credBytes,
		})
	}

	// Set up options
	ctapOptions := make(map[ctaptypes.Option]bool)
	if options.UserVerification == "required" {
		ctapOptions[ctaptypes.OptionUserPresence] = true
		ctapOptions[ctaptypes.OptionUserVerification] = true
	}

	fmt.Println("Touch your security key to authenticate...")

	// Get assertion from device using iterator
	// pinUvAuthToken is nil because we're not using PIN/UV auth protocol
	// We'll just use the first assertion
	var assertion *ctaptypes.AuthenticatorGetAssertionResponse
	for resp, err := range dev.GetAssertion(
		nil,            // pinUvAuthToken
		options.RPID,   // rpID
		clientDataHash, // clientData hash
		allowList,      // allowList
		nil,            // extInputs
		ctapOptions,    // options
	) {
		if err != nil {
			return nil, fmt.Errorf("failed to get assertion: %w", err)
		}
		assertion = resp
		break // Use first assertion
	}

	if assertion == nil {
		return nil, fmt.Errorf("no assertions returned from device")
	}

	// Extract user handle if present
	userHandle := ""
	if assertion.User != nil && len(assertion.User.ID) > 0 {
		userHandle = base64.RawURLEncoding.EncodeToString(assertion.User.ID)
	}

	return &AssertionResponse{
		CredentialID:      base64.RawURLEncoding.EncodeToString(assertion.Credential.ID),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(assertion.AuthDataRaw),
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataJSON),
		Signature:         base64.RawURLEncoding.EncodeToString(assertion.Signature),
		UserHandle:        userHandle,
	}, nil
}

// CheckAvailability returns true if WebAuthn is available on this system
func CheckAvailability() bool {
	devInfos, err := sugar.EnumerateFIDODevices()
	if err != nil {
		return false
	}
	return len(devInfos) > 0
}
