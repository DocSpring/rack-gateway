//go:build !nofido

package webauthn

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/keys-pub/go-libfido2"
)

// minPINLength is the shortest PIN a FIDO2 security key takes.
const minPINLength = 4

// pinKey is what checking a PIN before an assertion needs from a security key. *libfido2.Device implements it.
type pinKey interface {
	Info() (*libfido2.DeviceInfo, error)
	CredentialsInfo(pin string) (*libfido2.CredentialsInfo, error)
	RetryCount() (int, error)
}

// SingleKeyHasPIN reports whether exactly one security key is connected and it has a PIN set. Its PIN can then be
// asked for before the gateway sends a challenge: it's the key that will sign it. With several keys connected, the
// key to use is only known from the challenge's credentials.
func SingleKeyHasPIN() bool {
	keys, err := openPINKeys()
	return err == nil && onlyKeyHasPIN(keys)
}

// CheckPIN checks pin on the one connected security key, without a touch. A PIN the key refuses returns an error
// wrapping ErrWrongPIN, which says how many tries are left. A key that can't check a PIN this way (it has no
// credential management) returns nil, and the assertion checks the PIN.
func CheckPIN(pin string) error {
	keys, err := openPINKeys()
	if err != nil {
		return err
	}
	if len(keys) != 1 {
		return fmt.Errorf("%d security keys are connected: connect only the one you use with rack-gateway", len(keys))
	}
	return verifyPIN(keys[0], pin)
}

func openPINKeys() ([]pinKey, error) {
	devices, err := openDevices()
	if err != nil {
		return nil, err
	}
	keys := make([]pinKey, 0, len(devices))
	for _, device := range devices {
		keys = append(keys, device)
	}
	return keys, nil
}

func onlyKeyHasPIN(keys []pinKey) bool {
	if len(keys) != 1 {
		return false
	}
	info, err := keys[0].Info()
	return err == nil && optionEnabled(info, "clientPin")
}

// verifyPIN asks key for its credentials' metadata with pin, which needs the right PIN and no touch.
func verifyPIN(key pinKey, pin string) error {
	if utf8.RuneCountInString(pin) < minPINLength {
		return fmt.Errorf("%w: a PIN has at least %d characters", ErrWrongPIN, minPINLength)
	}
	_, err := key.CredentialsInfo(pin)
	if err == nil {
		return nil
	}
	if errors.Is(err, libfido2.ErrPinInvalid) {
		tries, countErr := key.RetryCount()
		if countErr != nil {
			return ErrWrongPIN
		}
		return fmt.Errorf("%w: %d tries left before the key locks", ErrWrongPIN, tries)
	}
	return pinError(err)
}
