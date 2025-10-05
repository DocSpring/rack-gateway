package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"

	"github.com/keys-pub/go-libfido2"
)

func main() {
	userVerification := flag.String("uv", "discouraged", "User verification: required, preferred, discouraged")
	pin := flag.String("pin", "", "PIN for the security key (required if your Yubikey has a PIN set)")
	flag.Parse()

	fmt.Println("Testing libfido2 device detection and assertion...")
	fmt.Printf("User verification mode: %s\n\n", *userVerification)

	// List devices
	locs, err := libfido2.DeviceLocations()
	if err != nil {
		log.Fatalf("Failed to get device locations: %v", err)
	}

	if len(locs) == 0 {
		log.Println("No FIDO2 devices found")
		return
	}

	fmt.Printf("Found %d device(s):\n", len(locs))
	for i, loc := range locs {
		fmt.Printf("  %d. Path: %s\n", i+1, loc.Path)
		fmt.Printf("     Product: %s\n", loc.Product)
		fmt.Printf("     Manufacturer: %s\n", loc.Manufacturer)

		// Try to open device
		device, err := libfido2.NewDevice(loc.Path)
		if err != nil {
			fmt.Printf("     Error opening device: %v\n", err)
			continue
		}

		// Get device info
		info, err := device.Info()
		if err != nil {
			fmt.Printf("     Error getting device info: %v\n", err)
			continue
		}

		fmt.Printf("     FIDO Version: %s\n", info.Versions)
		fmt.Printf("     Extensions: %s\n", info.Extensions)
		fmt.Printf("     AAGUID: %x\n", info.AAGUID)
		fmt.Printf("     Options: %+v\n", info.Options)

		// Test assertion
		fmt.Println("\n=== Testing WebAuthn Assertion ===")
		if err := testAssertion(device, *userVerification, *pin); err != nil {
			fmt.Printf("Assertion failed: %v\n", err)
			continue
		}
		fmt.Println("✓ Assertion succeeded!")
	}

	fmt.Println("\n✓ libfido2 is working correctly!")
}

func testAssertion(device *libfido2.Device, userVerification, pin string) error {
	// Generate a test challenge
	challenge := libfido2.RandBytes(32)
	rpID := "rack-gateway.test"

	// For testing, we need to first create a credential
	// In production, the credential would already exist from registration
	fmt.Println("Creating test credential...")
	userID := libfido2.RandBytes(32)

	// Determine options based on user verification setting
	// For "required", we set UV: True. For "discouraged", we leave opts nil (device default)
	var makeCredOpts *libfido2.MakeCredentialOpts
	if userVerification == "required" {
		makeCredOpts = &libfido2.MakeCredentialOpts{
			UV: libfido2.True,
		}
		fmt.Println("NOTE: UV required mode needs a PIN or biometric")
	}

	fmt.Printf("Touch your security key to create credential (UV: %s)...\n", userVerification)
	credential, err := device.MakeCredential(
		challenge,
		libfido2.RelyingParty{
			ID:   rpID,
			Name: "Rack Gateway Test",
		},
		libfido2.User{
			ID:          userID,
			Name:        "test-user",
			DisplayName: "Test User",
		},
		libfido2.ES256,
		pin,
		makeCredOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to create credential: %w", err)
	}

	fmt.Printf("✓ Credential created: %s\n", base64.RawURLEncoding.EncodeToString(credential.CredentialID))

	// Now test assertion with the credential
	fmt.Println("\nTesting assertion...")
	newChallenge := libfido2.RandBytes(32)

	// For "required", we set UV: True. For "discouraged", we leave opts nil (device default)
	var assertionOpts *libfido2.AssertionOpts
	if userVerification == "required" {
		assertionOpts = &libfido2.AssertionOpts{
			UV: libfido2.True,
		}
	}

	fmt.Printf("Touch your security key to sign assertion (UV: %s)...\n", userVerification)
	assertion, err := device.Assertion(
		rpID,
		newChallenge,
		[][]byte{credential.CredentialID},
		pin,
		assertionOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to get assertion: %w", err)
	}

	fmt.Printf("✓ Assertion signature: %x...\n", assertion.Sig[:min(32, len(assertion.Sig))])
	fmt.Printf("✓ Auth data CBOR: %x...\n", assertion.AuthDataCBOR[:min(32, len(assertion.AuthDataCBOR))])
	fmt.Printf("✓ Credential ID: %s\n", base64.RawURLEncoding.EncodeToString(assertion.CredentialID))

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
