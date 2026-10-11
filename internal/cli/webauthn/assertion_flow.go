//go:build !nofido

package webauthn

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/keys-pub/go-libfido2"
)

// infoRetryDelay is how long to wait before reading a security key's info a second time. A read can fail for a
// moment, for example while another app (a browser or password manager) is using the key.
const infoRetryDelay = 500 * time.Millisecond

// errUnauthorizedPermission is CTAP2_ERR_UNAUTHORIZED_PERMISSION, which go-libfido2 has no name for. A CTAP 2.1
// key without credential management returns it when asked for permission to list credentials.
var errUnauthorizedPermission = libfido2.Error{Code: 0x40}

var errNoPINEntered = errors.New(
	"no security key PIN entered; the gateway requires user verification, and this key verifies you with its PIN",
)

// securityKey is what getting an assertion needs from a FIDO2 device. *libfido2.Device implements it.
type securityKey interface {
	Info() (*libfido2.DeviceInfo, error)
	Credentials(rpID string, pin string) ([]*libfido2.Credential, error)
	Assertion(
		rpID string, clientDataHash []byte, credentialIDs [][]byte, pin string, opts *libfido2.AssertionOpts,
	) (*libfido2.Assertion, error)
}

// assertionFlow gets an assertion from one security key.
type assertionFlow struct {
	key securityKey
	// readPIN asks the user for the key's PIN.
	readPIN func() (string, error)
	// infoRetryDelay is the wait before reading the key's info again after a failed read.
	infoRetryDelay time.Duration
}

// getAssertion signs the challenge in options with the key. It returns the PIN it used, so later assertions in
// the same command can pass it as cachedPIN instead of asking again.
func (f assertionFlow) getAssertion(options AssertionOptions, cachedPIN string) (*AssertionResponse, string, error) {
	allowList, err := decodeCredentialIDs(options.AllowCredentials)
	if err != nil {
		return nil, "", err
	}

	pin, err := f.pinFor(options, cachedPIN)
	if err != nil {
		return nil, "", err
	}

	allowList, err = f.filterCredentials(options.RPID, allowList, pin)
	if err != nil {
		return nil, "", err
	}

	clientDataJSON, clientDataHash, err := buildClientData(options)
	if err != nil {
		return nil, "", err
	}

	promptTouch(options)
	assertion, err := f.key.Assertion(options.RPID, clientDataHash, allowList, pin, buildAssertionOptions(options))
	if err != nil {
		return nil, "", assertionError(err, pin)
	}

	resp, err := buildAssertionResponse(assertion, clientDataJSON)
	return resp, pin, err
}

// pinFor returns the PIN to send with the assertion: the cached one, or one the user enters when the key has a
// PIN set. A key that verifies users with its PIN (it has no built-in user verification, like a fingerprint reader)
// rejects a user-verified assertion without one, so when the key's info can't be read, or the user enters no PIN
// for such a key, it returns an error instead of going on without a PIN.
func (f assertionFlow) pinFor(options AssertionOptions, cachedPIN string) (string, error) {
	if cachedPIN != "" {
		return cachedPIN, nil
	}
	info, err := f.readInfo()
	if err != nil {
		return "", err
	}
	if !optionEnabled(info, "clientPin") {
		return "", nil
	}
	pin, err := f.readPIN()
	if err != nil {
		return "", err
	}
	if pin == "" && requiresUserVerification(options) && !optionEnabled(info, "uv") {
		return "", errNoPINEntered
	}
	return pin, nil
}

// readInfo reads the key's info, trying once more after infoRetryDelay if the first read fails.
func (f assertionFlow) readInfo() (*libfido2.DeviceInfo, error) {
	info, err := f.key.Info()
	if err == nil {
		return info, nil
	}
	<-time.After(f.infoRetryDelay)
	info, err = f.key.Info()
	if err != nil {
		return nil, keyUnreadableError(err)
	}
	return info, nil
}

// filterCredentials narrows the allow list to the credentials this key holds, and reports early when it holds
// none of them. Listing credentials needs the key's PIN. Without a PIN, or when the key can't list these
// credentials, the key gets the whole allow list and finds its credential itself.
func (f assertionFlow) filterCredentials(rpID string, allowList [][]byte, pin string) ([][]byte, error) {
	if pin == "" {
		return allowList, nil
	}
	deviceCreds, err := f.key.Credentials(rpID, pin)
	if err != nil {
		if cannotListCredentials(err) {
			return allowList, nil
		}
		return nil, fmt.Errorf("failed to list the security key's credentials: %w", err)
	}
	// Only discoverable credentials are listed. A non-discoverable one still works from the allow list.
	if len(deviceCreds) == 0 {
		return allowList, nil
	}

	filtered, err := intersectCredentialLists(allowList, deviceCreds)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "Found %d matching credential(s) on this device\n", len(filtered))
	return filtered, nil
}

// cannotListCredentials reports whether a failed listing means the key can't list these credentials, rather than
// that something went wrong: it has no credential management (an unknown command, option or permission), or no
// discoverable credential for this site.
func cannotListCredentials(err error) bool {
	return errors.Is(err, libfido2.ErrInvalidCommand) ||
		errors.Is(err, libfido2.ErrUnsupportedOption) ||
		errors.Is(err, errUnauthorizedPermission) ||
		errors.Is(err, libfido2.ErrNoCredentials)
}

// assertionError explains a failed assertion. libfido2 reports "unsupported option" when a key with no PIN set
// and no built-in user verification is asked to verify the user. It also reports it when the key didn't answer
// libfido2's info request while being opened: libfido2 then treats it as a U2F key, which can't take a PIN.
func assertionError(err error, pin string) error {
	if !errors.Is(err, libfido2.ErrUnsupportedOption) {
		return fmt.Errorf("failed to get assertion: %w", err)
	}
	if pin == "" {
		return fmt.Errorf("the security key can't verify you without a PIN (%w); set a PIN on it and try again", err)
	}
	return keyUnreadableError(err)
}

// keyUnreadableError explains a failure to talk to the key, which is usually another app using it.
func keyUnreadableError(err error) error {
	return fmt.Errorf("couldn't read the security key (%w); make sure no other app is using it and try again", err)
}

// optionEnabled reports whether the key's info sets the named option to true, e.g. "clientPin" when it has a PIN.
func optionEnabled(info *libfido2.DeviceInfo, name string) bool {
	for _, opt := range info.Options {
		if opt.Name == name && opt.Value == libfido2.True {
			return true
		}
	}
	return false
}
