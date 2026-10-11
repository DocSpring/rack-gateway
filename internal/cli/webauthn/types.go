// Package webauthn gets WebAuthn assertions from a FIDO2 security key (libfido2), or reports that this build
// has no security key support (the nofido build tag).
package webauthn

// AssertionOptions contains the challenge and credential info from the server
type AssertionOptions struct {
	Challenge        string   `json:"challenge"`
	RPID             string   `json:"rp_id"`
	AllowCredentials []string `json:"allow_credentials"`
	Timeout          int      `json:"timeout"`
	UserVerification string   `json:"user_verification"`
	Origin           string   `json:"origin"` // Gateway URL origin for clientData
	PINSummary       []string `json:"-"`      // What the assertion approves, one item per line, for the PIN dialog
}

// AssertionResponse contains the signed assertion to send back to the server
type AssertionResponse struct {
	CredentialID      string `json:"credential_id"`
	AuthenticatorData string `json:"authenticator_data"`
	ClientDataJSON    string `json:"client_data_json"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"user_handle"`
}
