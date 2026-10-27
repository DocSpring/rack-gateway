//go:build !nofido

package webauthn

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/keys-pub/go-libfido2"
)

// keyRetryDelay is how long to wait before asking a security key again after a request that can fail for a moment,
// for example while another app (a browser or password manager) is using the key.
const keyRetryDelay = 500 * time.Millisecond

// assertionAttempts is how many times getAssertion asks the key for an assertion that fails with libfido2's
// "unsupported option" although it was sent a PIN (see assertionError). The next try usually works.
const assertionAttempts = 3

// CTAP error codes go-libfido2 has no name for.
var (
	// errUnauthorizedPermission is CTAP2_ERR_UNAUTHORIZED_PERMISSION. A CTAP 2.1 key without credential management
	// returns it when asked for permission to list credentials.
	errUnauthorizedPermission = libfido2.Error{Code: 0x40}
	// errChannelBusy is CTAP1_ERR_CHANNEL_BUSY: another app is talking to the key.
	errChannelBusy = libfido2.Error{Code: 0x06}
	// errPINBlocked is CTAP2_ERR_PIN_BLOCKED: too many wrong PINs, so the key needs a reset.
	errPINBlocked = libfido2.Error{Code: 0x32}
)

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
	// retryDelay is the wait before asking the key again after a failed info read or assertion.
	retryDelay time.Duration
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

	assertion, err := f.assert(options, clientDataHash, allowList, pin)
	if err != nil {
		return nil, "", assertionError(err, pin)
	}

	resp, err := buildAssertionResponse(assertion, clientDataJSON)
	return resp, pin, err
}

// assert asks the key for the assertion, asking the user for a touch each time. When the key answers "unsupported
// option" although it was sent a PIN, it asks again with the same PIN, up to assertionAttempts times in all, so the
// user only touches the key again.
func (f assertionFlow) assert(
	options AssertionOptions, clientDataHash []byte, allowList [][]byte, pin string,
) (*libfido2.Assertion, error) {
	opts := buildAssertionOptions(options)
	for attempt := 1; ; attempt++ {
		promptTouch(options)
		assertion, err := f.key.Assertion(options.RPID, clientDataHash, allowList, pin, opts)
		if err == nil || pin == "" || !errors.Is(err, libfido2.ErrUnsupportedOption) || attempt == assertionAttempts {
			return assertion, err
		}
		fmt.Fprintf(os.Stderr, "The security key didn't answer (%v); trying again (%d of %d)\n",
			err, attempt+1, assertionAttempts)
		<-time.After(f.retryDelay)
	}
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

// readInfo reads the key's info, trying once more after retryDelay if the first read fails.
func (f assertionFlow) readInfo() (*libfido2.DeviceInfo, error) {
	info, err := f.key.Info()
	if err == nil {
		return info, nil
	}
	<-time.After(f.retryDelay)
	info, err = f.key.Info()
	if err != nil {
		return nil, keyUnreadableError(err)
	}
	return info, nil
}

// filterCredentials narrows the allow list to the credentials this key lists. Listing credentials needs the key's
// PIN. Without a PIN, when the key can't list these credentials, or when none it lists is in the allow list, the
// key gets the whole allow list and finds its credential itself.
func (f assertionFlow) filterCredentials(rpID string, allowList [][]byte, pin string) ([][]byte, error) {
	if pin == "" {
		return allowList, nil
	}
	deviceCreds, err := f.key.Credentials(rpID, pin)
	if err != nil {
		if pinErr := pinError(err); pinErr != nil {
			return nil, pinErr
		}
		if cannotListCredentials(err) {
			return allowList, nil
		}
		return nil, fmt.Errorf("failed to list the security key's credentials: %w", err)
	}
	// Only discoverable credentials are listed. A non-discoverable one still works from the allow list.
	filtered := matchingCredentials(allowList, deviceCreds)
	if len(filtered) == 0 {
		return allowList, nil
	}
	fmt.Fprintf(os.Stderr, "Found %d matching credential(s) on this device\n", len(filtered))
	return filtered, nil
}

// chooseKey returns the first of keys that holds a credential in allowList, or the first key when none says so.
// It asks each key for an assertion without user presence, which a key answers without a touch, and fails with
// "no credentials" when it holds none of them.
func chooseKey(keys []securityKey, rpID string, allowList [][]byte) securityKey {
	if len(keys) == 1 {
		return keys[0]
	}
	probe := &libfido2.AssertionOpts{UP: libfido2.False}
	for _, key := range keys {
		if _, err := key.Assertion(rpID, make([]byte, sha256.Size), allowList, "", probe); err == nil {
			return key
		}
	}
	return keys[0]
}

// cannotListCredentials reports whether a failed listing just means the key gets the whole allow list: it has no
// credential management (an unknown command, option or permission), it has no discoverable credential for this
// site, or the transfer failed for a moment. Listing only narrows the allow list, and the assertion opens the key
// again, so it reports a transfer problem that lasts.
func cannotListCredentials(err error) bool {
	for _, target := range []error{
		libfido2.ErrInvalidCommand,
		libfido2.ErrUnsupportedOption,
		errUnauthorizedPermission,
		libfido2.ErrNoCredentials,
		libfido2.ErrTX,
		libfido2.ErrRX,
		errChannelBusy,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// pinError explains a failure caused by the PIN, or returns nil for any other error.
func pinError(err error) error {
	switch {
	case errors.Is(err, libfido2.ErrPinInvalid):
		return fmt.Errorf("%w (%w); try again", ErrWrongPIN, err)
	case errors.Is(err, libfido2.ErrPinAuthBlocked):
		return fmt.Errorf("too many wrong PINs in a row (%w); unplug the security key, plug it back in and try again",
			err)
	case errors.Is(err, errPINBlocked):
		return fmt.Errorf("the security key's PIN is blocked after too many wrong PINs (%w); "+
			"the key can only be used again after a reset, which deletes its credentials", err)
	default:
		return nil
	}
}

// assertionError explains a failed assertion. libfido2 reports "unsupported option" when a key with no PIN set
// and no built-in user verification is asked to verify the user. It also reports it when the key didn't answer
// libfido2's info request while being opened: libfido2 then treats it as a U2F key, which can't take a PIN. With a
// PIN it's the second case, which usually passes, so assert has already tried again.
func assertionError(err error, pin string) error {
	if pinErr := pinError(err); pinErr != nil {
		return pinErr
	}
	if errors.Is(err, libfido2.ErrNoCredentials) {
		return fmt.Errorf("this security key holds none of your registered credentials for this gateway (%w); "+
			"use the key you registered", err)
	}
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
