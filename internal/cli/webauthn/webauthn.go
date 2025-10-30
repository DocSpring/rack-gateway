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
	device, err := openFirstFIDODevice()
	if err != nil {
		return nil, err
	}

	allowList, err := decodeCredentialIDs(options.AllowCredentials)
	if err != nil {
		return nil, err
	}

	allowList, pin, err := filterCredentialsForDevice(device, options.RPID, allowList)
	if err != nil {
		return nil, err
	}

	clientDataJSON, clientDataHash, err := buildClientData(options)
	if err != nil {
		return nil, err
	}

	assertionOpts := buildAssertionOptions(options)

	fmt.Fprintln(os.Stderr, "Touch your security key to authenticate...")
	assertion, err := device.Assertion(options.RPID, clientDataHash, allowList, pin, assertionOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to get assertion: %w", err)
	}

	return buildAssertionResponse(assertion, clientDataJSON)
}

func openFirstFIDODevice() (*libfido2.Device, error) {
	locs, err := libfido2.DeviceLocations()
	if err != nil {
		return nil, fmt.Errorf("failed to list FIDO2 devices: %w", err)
	}
	if len(locs) == 0 {
		return nil, fmt.Errorf("no FIDO2 devices found")
	}
	device, err := libfido2.NewDevice(locs[0].Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open FIDO2 device: %w", err)
	}
	return device, nil
}

func decodeCredentialIDs(ids []string) ([][]byte, error) {
	allowList := make([][]byte, 0, len(ids))
	for _, credID := range ids {
		credBytes, err := base64.RawURLEncoding.DecodeString(credID)
		if err != nil {
			return nil, fmt.Errorf("failed to decode credential ID: %w", err)
		}
		allowList = append(allowList, credBytes)
	}
	return allowList, nil
}

func filterCredentialsForDevice(device *libfido2.Device, rpID string, allowList [][]byte) ([][]byte, string, error) {
	info, err := device.Info()
	if err != nil {
		return allowList, "", nil
	}

	pin, err := maybePromptForPIN(info)
	if err != nil {
		return nil, "", err
	}

	deviceCreds, err := device.Credentials(rpID, pin)
	if err != nil {
		return allowList, pin, nil
	}

	if len(deviceCreds) == 0 {
		return nil, "", fmt.Errorf("this security key has no credentials registered for this service")
	}

	filtered, err := intersectCredentialLists(allowList, deviceCreds)
	if err != nil {
		return nil, "", err
	}
	fmt.Fprintf(os.Stderr, "Found %d matching credential(s) on this device\n", len(filtered))
	return filtered, pin, nil
}

func maybePromptForPIN(info *libfido2.DeviceInfo) (string, error) {
	for _, opt := range info.Options {
		if opt.Name == "clientPin" && opt.Value == libfido2.True {
			fmt.Fprint(os.Stderr, "Enter your security key PIN: ")
			pinBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return "", fmt.Errorf("failed to read PIN: %w", err)
			}
			return string(pinBytes), nil
		}
	}
	return "", nil
}

func intersectCredentialLists(allowList [][]byte, deviceCreds []*libfido2.Credential) ([][]byte, error) {
	deviceCredMap := make(map[string]bool, len(deviceCreds))
	for _, cred := range deviceCreds {
		encoded := base64.RawURLEncoding.EncodeToString(cred.ID)
		deviceCredMap[encoded] = true
	}

	filtered := make([][]byte, 0, len(allowList))
	for _, credBytes := range allowList {
		credID := base64.RawURLEncoding.EncodeToString(credBytes)
		if deviceCredMap[credID] {
			filtered = append(filtered, credBytes)
		}
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("none of your registered credentials are on this device (device has %d credential(s) for this service, but none match)", len(deviceCreds))
	}

	return filtered, nil
}

func buildClientData(options AssertionOptions) ([]byte, []byte, error) {
	origin := options.Origin
	if origin == "" {
		origin = "http://localhost"
	}
	clientData := map[string]interface{}{
		"type":      "webauthn.get",
		"challenge": options.Challenge,
		"origin":    origin,
	}
	clientDataJSON, err := json.Marshal(clientData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal client data: %w", err)
	}

	h := sha256.New()
	h.Write(clientDataJSON)
	clientDataHash := h.Sum(nil)
	return clientDataJSON, clientDataHash, nil
}

func buildAssertionOptions(options AssertionOptions) *libfido2.AssertionOpts {
	if options.UserVerification != "required" {
		return nil
	}
	return &libfido2.AssertionOpts{UV: libfido2.True}
}

func buildAssertionResponse(assertion *libfido2.Assertion, clientDataJSON []byte) (*AssertionResponse, error) {
	userHandle := ""
	if len(assertion.User.ID) > 0 {
		userHandle = base64.RawURLEncoding.EncodeToString(assertion.User.ID)
	}

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
