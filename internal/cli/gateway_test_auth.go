package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

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
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := SelectedRack()
			if err != nil {
				return err
			}

			gatewayURL, bearer, err := gatewayAuthInfo(rack)
			if err != nil {
				return err
			}

			normalized, err := NormalizeGatewayURL(gatewayURL)
			if err != nil {
				return err
			}

			// Determine test mode
			testMode := "basic"
			if len(args) > 0 {
				testMode = strings.ToLower(args[0])
			}

			// Test basic auth by getting MFA status
			fmt.Printf("Testing authentication to %s...\n", normalized)
			mfaStatus, err := getMFAStatus(normalized, bearer)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}

			fmt.Printf("✓ Authentication successful\n")
			fmt.Printf("  User is enrolled in MFA: %v\n", mfaStatus.Enrolled)
			fmt.Printf("  MFA required: %v\n", mfaStatus.Required)

			if len(mfaStatus.Methods) > 0 {
				fmt.Printf("  Enrolled methods:\n")
				for _, method := range mfaStatus.Methods {
					label := method.Label
					if label == "" {
						label = method.Type
					}
					status := ""
					if method.IsEnrolling {
						status = " (enrolling)"
					}
					fmt.Printf("    - %s (%s)%s\n", label, method.Type, status)
				}
			}

			// If just basic test, we're done
			if testMode == "basic" {
				return nil
			}

			// Test MFA flows
			if !mfaStatus.Enrolled {
				return fmt.Errorf("MFA not enrolled - cannot test MFA flows")
			}

			switch testMode {
			case "mfa":
				return testPreferredMFA(cmd, normalized, bearer, mfaStatus, rack)
			case "otp", "totp":
				return testOTPAuth(cmd, normalized, bearer, mfaStatus)
			case "webauthn":
				return testWebAuthnAuth(normalized, bearer, mfaStatus)
			default:
				return fmt.Errorf("unknown test mode: %s (valid options: mfa, otp, webauthn)", testMode)
			}
		}),
	}

	return cmd
}

func testPreferredMFA(cmd *cobra.Command, baseURL, bearer string, status *MFAStatusResponse, rack string) error {
	if len(status.Methods) == 0 {
		return fmt.Errorf("no MFA methods enrolled")
	}

	fmt.Println("\nTesting preferred MFA method...")

	// Check for --mfa-method flag override
	if MFAMethodFlag != "" {
		var overrideMethod *MFAMethodResponse
		for _, method := range status.Methods {
			if method.Type == MFAMethodFlag && !method.IsEnrolling {
				overrideMethod = &method
				break
			}
		}
		if overrideMethod == nil {
			return fmt.Errorf("MFA method %q not found or not enrolled", MFAMethodFlag)
		}
		fmt.Printf("  Using --mfa-method override: %s\n", MFAMethodFlag)
		fmt.Printf("\nAttempting %s authentication...\n", overrideMethod.Type)
		if err := testMFAMethod(cmd, baseURL, bearer, *overrideMethod, status.Methods); err != nil {
			return fmt.Errorf("%s failed: %v", overrideMethod.Type, err)
		}
		fmt.Printf("✓ MFA verification successful with %s\n", overrideMethod.Type)
		return nil
	}

	// Use server's preferred method if set
	var preferredMethod *MFAMethodResponse
	if status.PreferredMethod != nil && *status.PreferredMethod != "" {
		for _, method := range status.Methods {
			if method.Type == *status.PreferredMethod && !method.IsEnrolling {
				preferredMethod = &method
				break
			}
		}
	}

	// If server has a preferred method, use it
	if preferredMethod != nil {
		fmt.Printf("  Server preferred method: %s\n", preferredMethod.Type)
		fmt.Printf("\nAttempting %s authentication...\n", preferredMethod.Type)
		if err := testMFAMethod(cmd, baseURL, bearer, *preferredMethod, status.Methods); err != nil {
			return fmt.Errorf("%s failed: %v", preferredMethod.Type, err)
		}
		fmt.Printf("✓ MFA verification successful with %s\n", preferredMethod.Type)
		return nil
	}

	// Otherwise fall back to CLI preference
	cfg, _, err := LoadConfig()
	if err != nil {
		cfg = &Config{MFAPreference: "default"}
	}

	preference := cfg.MFAPreference
	if rack != "" {
		if gateway, ok := cfg.Gateways[rack]; ok && gateway.MFAPreference != "" {
			preference = gateway.MFAPreference
		}
	}

	fmt.Printf("  No server preference - using CLI preference: %s\n", preference)

	// Try methods based on preference
	methods := filterMethodsByPreference(status.Methods, preference)

	if len(methods) == 0 {
		return fmt.Errorf("no MFA methods available (preference: %q)", preference)
	}

	for _, method := range methods {
		fmt.Printf("\nAttempting %s authentication...\n", method.Type)
		if err := testMFAMethod(cmd, baseURL, bearer, method, status.Methods); err != nil {
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

func testMFAMethod(cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, allMethods []MFAMethodResponse) error {
	// Use unified MFA verification module
	return verifyMFAMethod(cmd, baseURL, bearer, method, allMethods)
}
