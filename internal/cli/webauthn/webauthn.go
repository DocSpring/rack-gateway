package webauthn

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/fxamacker/cbor/v2"
	"github.com/keys-pub/go-libfido2"
	"golang.org/x/term"
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
	// List FIDO2 devices
	locs, err := libfido2.DeviceLocations()
	if err != nil {
		return nil, fmt.Errorf("failed to list FIDO2 devices: %w", err)
	}
	if len(locs) == 0 {
		return nil, fmt.Errorf("no FIDO2 devices found")
	}

	// Use first device
	device, err := libfido2.NewDevice(locs[0].Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open FIDO2 device: %w", err)
	}

	// Decode allowed credentials first
	var allowList [][]byte
	for _, credID := range options.AllowCredentials {
		credBytes, err := base64.RawURLEncoding.DecodeString(credID)
		if err != nil {
			return nil, fmt.Errorf("failed to decode credential ID: %w", err)
		}
		allowList = append(allowList, credBytes)
	}

	// Check if device requires PIN and filter credentials if possible
	// This requires credential management support (CTAP 2.1+)
	pin := ""
	info, err := device.Info()
	if err == nil {
		// Check if clientPin is required
		pinRequired := false
		for _, opt := range info.Options {
			if opt.Name == "clientPin" && opt.Value == libfido2.True {
				pinRequired = true
				break
			}
		}

		if pinRequired {
			// Need PIN to enumerate credentials - ask for it now
			fmt.Print("Enter your security key PIN: ")
			pinBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println() // New line after password input
			if err != nil {
				return nil, fmt.Errorf("failed to read PIN: %w", err)
			}
			pin = string(pinBytes)
		}

		// Try to enumerate credentials to filter the allowList
		deviceCreds, err := device.Credentials(options.RPID, pin)
		if err == nil {
			if len(deviceCreds) == 0 {
				// Device has no credentials for this RP - fail immediately
				return nil, fmt.Errorf("this security key has no credentials registered for this service")
			}

			// Build map of device credential IDs
			deviceCredMap := make(map[string]bool)
			for _, cred := range deviceCreds {
				deviceCredMap[base64.RawURLEncoding.EncodeToString(cred.ID)] = true
			}

			// Filter allowList to only credentials on this device
			var filteredAllowList [][]byte
			for _, credBytes := range allowList {
				credID := base64.RawURLEncoding.EncodeToString(credBytes)
				if deviceCredMap[credID] {
					filteredAllowList = append(filteredAllowList, credBytes)
				}
			}

			if len(filteredAllowList) == 0 {
				return nil, fmt.Errorf("none of your registered credentials are on this device (device has %d credential(s) for this service, but none match)", len(deviceCreds))
			}

			allowList = filteredAllowList
			fmt.Printf("Found %d matching credential(s) on this device\n", len(allowList))
		}
		// If credential enumeration fails, proceed with full allowList
	}

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

	// Set up options
	var assertionOpts *libfido2.AssertionOpts
	if options.UserVerification == "required" {
		assertionOpts = &libfido2.AssertionOpts{
			UV: libfido2.True,
		}
	}

	fmt.Println("Touch your security key to authenticate...")

	// Get assertion from device
	assertion, err := device.Assertion(
		options.RPID,
		clientDataHash,
		allowList,
		pin,
		assertionOpts,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get assertion: %w", err)
	}

	// Extract user handle if present
	userHandle := ""
	if len(assertion.User.ID) > 0 {
		userHandle = base64.RawURLEncoding.EncodeToString(assertion.User.ID)
	}

	// Decode CBOR to get raw authenticator data
	// libfido2 returns AuthDataCBOR which is a CBOR-encoded byte string
	// We need to decode it to get the raw authenticator data bytes
	var rawAuthData []byte
	if err := cbor.Unmarshal(assertion.AuthDataCBOR, &rawAuthData); err != nil {
		return nil, fmt.Errorf("failed to decode CBOR auth data: %w", err)
	}

	return &AssertionResponse{
		CredentialID:      base64.RawURLEncoding.EncodeToString(assertion.CredentialID),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(rawAuthData),
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataJSON),
		Signature:         base64.RawURLEncoding.EncodeToString(assertion.Sig),
		UserHandle:        userHandle,
	}, nil
}

// CheckAvailability returns true if WebAuthn is available on this system
func CheckAvailability() bool {
	locs, err := libfido2.DeviceLocations()
	if err != nil {
		return false
	}
	return len(locs) > 0
}
