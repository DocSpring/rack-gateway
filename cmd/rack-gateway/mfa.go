package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// performMFAStepUp handles MFA verification for step-up authentication scenarios
// (e.g., deploy approvals, sensitive operations). It respects --mfa-method flag,
// server preferences, and CLI preferences.
func performMFAStepUp(cmd *cobra.Command, baseURL, bearer, rack string) error {
	// Get MFA status to see what methods are available
	mfaStatus, err := getMFAStatus(baseURL, bearer)
	if err != nil {
		return fmt.Errorf("failed to check MFA status: %w", err)
	}

	if !mfaStatus.Enrolled {
		return fmt.Errorf("MFA not enrolled - step-up authentication requires MFA")
	}

	if len(mfaStatus.Methods) == 0 {
		return fmt.Errorf("no MFA methods enrolled")
	}

	// Check for --mfa-method flag override
	if mfaMethodFlag != "" {
		var overrideMethod *mfaMethodResponse
		for _, method := range mfaStatus.Methods {
			if method.Type == mfaMethodFlag && !method.IsEnrolling {
				overrideMethod = &method
				break
			}
		}
		if overrideMethod == nil {
			return fmt.Errorf("MFA method %q not found or not enrolled", mfaMethodFlag)
		}
		return verifyMFAMethod(cmd, baseURL, bearer, *overrideMethod, mfaStatus.Methods)
	}

	// Use server's preferred method if set
	var preferredMethod *mfaMethodResponse
	if mfaStatus.PreferredMethod != nil && *mfaStatus.PreferredMethod != "" {
		for _, method := range mfaStatus.Methods {
			if method.Type == *mfaStatus.PreferredMethod && !method.IsEnrolling {
				preferredMethod = &method
				break
			}
		}
	}

	// If server has a preferred method, use it
	if preferredMethod != nil {
		return verifyMFAMethod(cmd, baseURL, bearer, *preferredMethod, mfaStatus.Methods)
	}

	// Otherwise fall back to CLI preference
	cfg, _, err := loadConfig()
	if err != nil {
		cfg = &Config{MFAPreference: "default"}
	}

	preference := cfg.MFAPreference
	if rack != "" {
		if gateway, ok := cfg.Gateways[rack]; ok && gateway.MFAPreference != "" {
			preference = gateway.MFAPreference
		}
	}

	// Try methods based on preference
	methods := filterMethodsByPreference(mfaStatus.Methods, preference)

	if len(methods) == 0 {
		return fmt.Errorf("no MFA methods available (preference: %q)", preference)
	}

	// Try each method in preference order
	var lastErr error
	for _, method := range methods {
		if err := verifyMFAMethod(cmd, baseURL, bearer, method, mfaStatus.Methods); err != nil {
			lastErr = err
			continue
		}
		// Success!
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("MFA verification failed: %w", lastErr)
	}
	return fmt.Errorf("all MFA methods failed")
}

// verifyMFAMethod handles verification for a single MFA method
// It outputs appropriate prompts and handles WebAuthn and TOTP flows
func verifyMFAMethod(cmd *cobra.Command, baseURL, bearer string, method mfaMethodResponse, allMethods []mfaMethodResponse) error {
	out := cmd.OutOrStdout()

	switch method.Type {
	case "webauthn":
		// Show alternative method hint if available
		if hasMethodType(allMethods, "totp") {
			if err := writeLine(out, "Tip: Pass `--mfa-method totp` to use your authenticator app instead."); err != nil {
				return err
			}
		}

		if err := writeLine(out, "Multi-factor authentication required."); err != nil {
			return err
		}

		if err := tryWebAuthnVerification(baseURL, bearer); err != nil {
			return fmt.Errorf("WebAuthn verification failed: %w", err)
		}

		if err := writeLine(out, "MFA verified."); err != nil {
			return err
		}
		return nil

	case "totp":
		// Show alternative method hint if available
		if hasMethodType(allMethods, "webauthn") {
			if err := writeLine(out, "Tip: Pass `--mfa-method webauthn` to use your security key instead."); err != nil {
				return err
			}
		}

		if err := writeLine(out, "Multi-factor authentication required."); err != nil {
			return err
		}

		// Try up to 5 times
		for attempts := 0; attempts < 5; attempts++ {
			code, err := promptMFACode()
			if err != nil {
				return err
			}
			if code == "" {
				if err := writeLine(out, "MFA code cannot be empty."); err != nil {
					return err
				}
				continue
			}
			if err := submitMFAVerification(baseURL, bearer, code); err != nil {
				if err := writef(out, "MFA verification failed: %v\n", err); err != nil {
					return err
				}
				continue
			}
			if err := writeLine(out, "MFA verified."); err != nil {
				return err
			}
			return nil
		}
		return errors.New("failed to verify MFA after multiple attempts")

	case "backup_code":
		if err := writeLine(out, "Multi-factor authentication required."); err != nil {
			return err
		}

		code, err := promptMFACode()
		if err != nil {
			return err
		}
		if err := submitMFAVerification(baseURL, bearer, code); err != nil {
			return fmt.Errorf("backup code verification failed: %w", err)
		}

		if err := writeLine(out, "MFA verified."); err != nil {
			return err
		}
		return nil

	default:
		return fmt.Errorf("unsupported MFA method: %s", method.Type)
	}
}
