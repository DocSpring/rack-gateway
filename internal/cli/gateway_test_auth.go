package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// TestAuthCommand returns the cobra command for testing authentication and MFA flows.
func TestAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test-auth [mfa]",
		Short: "Test authentication (optionally with MFA)",
		Long: `Test that authentication is working correctly.

Examples:
  rack-gateway test-auth                            # Test basic authentication
  rack-gateway test-auth mfa                        # Test auth + preferred MFA method
  rack-gateway test-auth mfa --mfa-method webauthn  # Override preferred method`,
		Args: cobra.MaximumNArgs(1),
		RunE: SilenceOnError(runTestAuth),
	}

	return cmd
}

func runTestAuth(cmd *cobra.Command, args []string) error {
	rack, err := SelectedRack()
	if err != nil {
		return err
	}

	normalized, bearer, status, err := loadAuthStatus(rack)
	if err != nil {
		return err
	}

	displayMFAStatus(status)

	testMode := determineTestMode(args)
	if testMode == "basic" {
		return nil
	}

	if !status.Enrolled {
		return fmt.Errorf("MFA not enrolled - cannot test MFA flows")
	}

	return runMFATestMode(cmd, testMode, normalized, bearer, status, rack)
}

func loadAuthStatus(rack string) (string, string, *MFAStatusResponse, error) {
	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", "", nil, err
	}

	normalized, err := NormalizeGatewayURL(gatewayURL)
	if err != nil {
		return "", "", nil, err
	}

	fmt.Printf("Testing authentication to %s...\n", normalized)
	status, err := getMFAStatus(normalized, bearer)
	if err != nil {
		return "", "", nil, fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Printf("✓ Authentication successful\n")
	return normalized, bearer, status, nil
}

func displayMFAStatus(status *MFAStatusResponse) {
	fmt.Printf("  User is enrolled in MFA: %v\n", status.Enrolled)
	fmt.Printf("  MFA required: %v\n", status.Required)

	if len(status.Methods) == 0 {
		return
	}

	fmt.Printf("  Enrolled methods:\n")
	for _, method := range status.Methods {
		label := method.Label
		if label == "" {
			label = method.Type
		}
		suffix := ""
		if method.IsEnrolling {
			suffix = " (enrolling)"
		}
		fmt.Printf("    - %s (%s)%s\n", label, method.Type, suffix)
	}
}

func determineTestMode(args []string) string {
	if len(args) == 0 {
		return "basic"
	}
	return strings.ToLower(args[0])
}

func runMFATestMode(cmd *cobra.Command, mode, baseURL, bearer string, status *MFAStatusResponse, rack string) error {
	switch mode {
	case "mfa":
		return testPreferredMFA(cmd, baseURL, bearer, status, rack)
	case "otp", "totp":
		return testOTPAuth(cmd, baseURL, bearer, status)
	case "webauthn":
		return testWebAuthnAuth(baseURL, bearer, status)
	default:
		return fmt.Errorf("unknown test mode: %s (valid options: mfa, otp, webauthn)", mode)
	}
}

func testPreferredMFA(cmd *cobra.Command, baseURL, bearer string, status *MFAStatusResponse, rack string) error {
	methods := availableMFAMethods(status.Methods)
	if len(methods) == 0 {
		return fmt.Errorf("no MFA methods enrolled")
	}

	fmt.Println("\nTesting preferred MFA method...")

	if MFAMethodFlag != "" {
		method, ok := overrideMFAMethod(methods)
		if !ok {
			return fmt.Errorf("MFA method %q not found or not enrolled", MFAMethodFlag)
		}
		fmt.Printf("  Using --mfa-method override: %s\n", method.Type)
		return runMFAMethod(cmd, baseURL, bearer, method, status.Methods)
	}

	if method, ok := preferredMFAMethod(status); ok {
		if method.IsEnrolling {
			return fmt.Errorf("preferred MFA method %q is still enrolling", method.Type)
		}
		fmt.Printf("  Server preferred method: %s\n", method.Type)
		return runMFAMethod(cmd, baseURL, bearer, method, status.Methods)
	}

	preference := resolveMFAPreference(rack)
	fmt.Printf("  No server preference - using CLI preference: %s\n", preference)

	ordered := filterMethodsByPreference(methods, preference)
	if len(ordered) == 0 {
		return fmt.Errorf("no MFA methods available (preference: %q)", preference)
	}

	return tryMFAMethods(cmd, baseURL, bearer, ordered, status.Methods)
}

func availableMFAMethods(methods []MFAMethodResponse) []MFAMethodResponse {
	filtered := make([]MFAMethodResponse, 0, len(methods))
	for _, method := range methods {
		if !method.IsEnrolling {
			filtered = append(filtered, method)
		}
	}
	return filtered
}

func runMFAMethod(cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, all []MFAMethodResponse) error {
	fmt.Printf("\nAttempting %s authentication...\n", method.Type)
	if err := testMFAMethod(cmd, baseURL, bearer, method, all); err != nil {
		return fmt.Errorf("%s failed: %v", method.Type, err)
	}
	fmt.Printf("✓ MFA verification successful with %s\n", method.Type)
	return nil
}

func tryMFAMethods(
	cmd *cobra.Command,
	baseURL, bearer string,
	ordered []MFAMethodResponse,
	all []MFAMethodResponse,
) error {
	for _, method := range ordered {
		fmt.Printf("\nAttempting %s authentication...\n", method.Type)
		if err := testMFAMethod(cmd, baseURL, bearer, method, all); err != nil {
			fmt.Printf("  ✗ %s failed: %v\n", method.Type, err)
			continue
		}
		fmt.Printf("✓ MFA verification successful with %s\n", method.Type)
		return nil
	}
	return fmt.Errorf("all MFA methods failed")
}

func testOTPAuth(cmd *cobra.Command, baseURL, bearer string, status *MFAStatusResponse) error {
	// Find TOTP method
	var totpMethod *MFAMethodResponse
	for _, method := range status.Methods {
		if method.Type == "totp" && !method.IsEnrolling {
			totpMethod = &method
			break
		}
	}

	if totpMethod == nil {
		return fmt.Errorf("TOTP/OTP not configured")
	}

	fmt.Printf("\nTesting TOTP authentication with: %s\n", totpMethod.Label)
	return testMFAMethod(cmd, baseURL, bearer, *totpMethod, status.Methods)
}

func testWebAuthnAuth(baseURL, bearer string, status *MFAStatusResponse) error {
	// Check WebAuthn availability
	if !checkWebAuthnAvailability() {
		return fmt.Errorf("no WebAuthn device detected")
	}

	// Find WebAuthn method
	var webauthnMethod *MFAMethodResponse
	for _, method := range status.Methods {
		if method.Type == "webauthn" && !method.IsEnrolling {
			webauthnMethod = &method
			break
		}
	}

	if webauthnMethod == nil {
		return fmt.Errorf("WebAuthn not configured")
	}

	fmt.Printf("\nTesting WebAuthn authentication with: %s\n", webauthnMethod.Label)
	if err := tryWebAuthnVerification(baseURL, bearer); err != nil {
		return err
	}

	fmt.Println("✓ WebAuthn verification successful")
	return nil
}

func testMFAMethod(
	cmd *cobra.Command,
	baseURL, bearer string,
	method MFAMethodResponse,
	allMethods []MFAMethodResponse,
) error {
	// Use unified MFA verification module
	return verifyMFAMethod(cmd, baseURL, bearer, method, allMethods)
}
