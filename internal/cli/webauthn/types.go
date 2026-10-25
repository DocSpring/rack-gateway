// Package webauthn gets WebAuthn assertions from a FIDO2 security key (libfido2), or reports that this build
// has no security key support (the nofido build tag).
package webauthn

import (
	"errors"
	"net/url"
)

// ErrWrongPIN is a security key PIN the key refused.
var ErrWrongPIN = errors.New("wrong security key PIN")

// AssertionOptions contains the challenge and credential info from the server
type AssertionOptions struct {
	Challenge        string   `json:"challenge"`
	RPID             string   `json:"rp_id"`
	AllowCredentials []string `json:"allow_credentials"`
	Timeout          int      `json:"timeout"`
	UserVerification string   `json:"user_verification"`
	Origin           string   `json:"origin"` // Gateway URL origin for clientData
	Prompt           Prompt   `json:"-"`
}

// Prompt tells the user what an assertion authorizes. None of it is secret: it's shown on screen.
type Prompt struct {
	// Summary lists what the command approves, one item per line, for the PIN dialog.
	Summary []string
	// Touch says what this touch is for, e.g. "approve api on staging (2 of 4)". Without it the touch is to
	// "authenticate to" the gateway.
	Touch string
	// Notify shows a desktop notification each time a touch is needed.
	Notify bool
}

// TouchReason says what a touch for these options is for, e.g. "approve api on staging (2 of 4)".
func (o AssertionOptions) TouchReason() string {
	if o.Prompt.Touch != "" {
		return o.Prompt.Touch
	}
	if u, err := url.Parse(o.Origin); err == nil && u.Host != "" {
		return "authenticate to " + u.Host
	}
	return "authenticate"
}

// AssertionResponse contains the signed assertion to send back to the server
type AssertionResponse struct {
	CredentialID      string `json:"credential_id"`
	AuthenticatorData string `json:"authenticator_data"`
	ClientDataJSON    string `json:"client_data_json"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"user_handle"`
}
