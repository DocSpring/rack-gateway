//go:build !nofido

package webauthn

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/fxamacker/cbor/v2"
	"github.com/keys-pub/go-libfido2"

	"github.com/DocSpring/rack-gateway/internal/cli/pinentry"
	"github.com/DocSpring/rack-gateway/internal/cli/touchnotify"
)

// GetAssertion prompts the user to authenticate with their FIDO2 device
func GetAssertion(options AssertionOptions) (*AssertionResponse, error) {
	resp, _, err := GetAssertionWithCachedPIN(options, "")
	return resp, err
}

// GetAssertionWithCachedPIN authenticates with FIDO2 device, optionally using a cached PIN.
// Returns the assertion response and the PIN used (for caching in subsequent calls).
// If cachedPIN is empty, asks for the PIN when the key has one. Otherwise uses the cached PIN.
// It holds the security key lock throughout (see withKeyLock).
func GetAssertionWithCachedPIN(options AssertionOptions, cachedPIN string) (*AssertionResponse, string, error) {
	var pin string
	resp, err := withKeyLock(func() (*AssertionResponse, error) {
		var resp *AssertionResponse
		var err error
		resp, pin, err = requestAssertion(options, cachedPIN)
		return resp, err
	})
	return resp, pin, err
}

// requestAssertion is GetAssertionWithCachedPIN, without the lock.
func requestAssertion(options AssertionOptions, cachedPIN string) (*AssertionResponse, string, error) {
	allowList, err := decodeCredentialIDs(options.AllowCredentials)
	if err != nil {
		return nil, "", err
	}
	keys, err := openFIDODevices()
	if err != nil {
		return nil, "", err
	}

	pinRequest := pinentry.Request{
		Gateway: options.Origin, Args: os.Args, Summary: options.Prompt.Summary, Warnings: options.Prompt.Warnings,
	}
	flow := assertionFlow{
		key:        chooseKey(keys, options.RPID, allowList),
		readPIN:    func() (string, error) { return pinentry.ReadPIN(pinRequest) },
		retryDelay: keyRetryDelay,
	}
	return flow.getAssertion(options, cachedPIN)
}

// promptTouch tells the user a touch is needed, every time one is: on the terminal, and in a desktop notification
// unless they're turned off.
func promptTouch(options AssertionOptions) {
	reason := options.TouchReason()
	fmt.Fprintf(os.Stderr, "Touch your security key to %s... (press Ctrl+C to abort)\n", reason)
	if options.Prompt.Notify {
		touchnotify.Show("To " + reason)
	}
}

// openFIDODevices opens every connected security key that can be opened.
func openFIDODevices() ([]securityKey, error) {
	devices, err := openDevices()
	if err != nil {
		return nil, err
	}
	keys := make([]securityKey, 0, len(devices))
	for _, device := range devices {
		keys = append(keys, device)
	}
	return keys, nil
}

// openDevices opens every connected FIDO2 device that can be opened. Tests replace it.
var openDevices = func() ([]*libfido2.Device, error) {
	locs, err := libfido2.DeviceLocations()
	if err != nil {
		return nil, fmt.Errorf("failed to list FIDO2 devices: %w", err)
	}
	if len(locs) == 0 {
		return nil, fmt.Errorf("no FIDO2 devices found")
	}
	devices := make([]*libfido2.Device, 0, len(locs))
	var openErr error
	for _, loc := range locs {
		device, err := libfido2.NewDevice(loc.Path)
		if err != nil {
			openErr = err
			continue
		}
		devices = append(devices, device)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("failed to open FIDO2 device: %w", openErr)
	}
	return devices, nil
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

// matchingCredentials returns the credentials in allowList that are among deviceCreds.
func matchingCredentials(allowList [][]byte, deviceCreds []*libfido2.Credential) [][]byte {
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

	return filtered
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
	if !requiresUserVerification(options) {
		return nil
	}
	return &libfido2.AssertionOpts{UV: libfido2.True}
}

func requiresUserVerification(options AssertionOptions) bool {
	return options.UserVerification == "required"
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
