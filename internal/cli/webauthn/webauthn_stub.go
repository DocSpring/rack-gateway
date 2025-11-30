//go:build nofido

package webauthn

import "fmt"

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

// GetAssertion stub - WebAuthn not available in this build
func GetAssertion(options AssertionOptions) (*AssertionResponse, error) {
	return nil, fmt.Errorf("WebAuthn support not compiled in this build (use -tags without nofido)")
}

// GetAssertionWithCachedPIN stub - WebAuthn not available in this build
func GetAssertionWithCachedPIN(options AssertionOptions, cachedPIN string) (*AssertionResponse, string, error) {
	return nil, "", fmt.Errorf("WebAuthn support not compiled in this build (use -tags without nofido)")
}

// CheckAvailability stub - always returns false
func CheckAvailability() bool {
	return false
}
