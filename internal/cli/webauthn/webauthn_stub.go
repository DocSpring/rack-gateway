//go:build nofido

package webauthn

import "fmt"

// Supported reports whether this build can use security keys. This one was built with the nofido tag.
const Supported = false

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

// SingleKeyHasPIN stub - no security key support in this build
func SingleKeyHasPIN() bool {
	return false
}

// CheckPIN stub - no security key support in this build
func CheckPIN(string) error {
	return nil
}
