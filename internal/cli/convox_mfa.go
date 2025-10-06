package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/spf13/cobra"
)

// checkMFAAndGetAuth checks if the command requires MFA and returns the auth string
// Returns: mfaAuth string in format "totp:code" or "webauthn:assertion" or "" for no MFA
func checkMFAAndGetAuth(cmd *cobra.Command, commandName string) (string, error) {
	// Look up the command to get its permissions
	convoxCmd, ok := convox.LookupCommand(commandName)
	if !ok {
		// Unknown command, let it through (will fail at API level if invalid)
		return "", nil
	}

	// Get rack and gateway info
	rack, err := SelectedRack()
	if err != nil {
		return "", err
	}

	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", err
	}

	// Check if MFA is required
	if convox.RequiresMFAAlways(convoxCmd.Permissions) {
		// MFAAlways - prompt for MFA and return it in the auth string
		return promptMFAForCommand(cmd, gatewayURL, bearer, rack)
	} else if convox.RequiresMFAStepUp(convoxCmd.Permissions) {
		// MFAStepUp - perform async step-up, no need to return MFA in auth
		if err := performMFAStepUp(cmd, gatewayURL, bearer, rack); err != nil {
			return "", err
		}
		return "", nil
	}

	// No MFA required
	return "", nil
}

// promptMFAForCommand prompts the user for MFA and returns the auth string
// Returns format: "totp:123456" or "webauthn:assertion_data"
func promptMFAForCommand(cmd *cobra.Command, baseURL, bearer, rack string) (string, error) {
	// Get MFA status to see what methods are available
	mfaStatus, err := getMFAStatus(baseURL, bearer)
	if err != nil {
		return "", fmt.Errorf("failed to check MFA status: %w", err)
	}

	if !mfaStatus.Enrolled {
		return "", fmt.Errorf("MFA not enrolled - this command requires MFA")
	}

	if len(mfaStatus.Methods) == 0 {
		return "", fmt.Errorf("no MFA methods enrolled")
	}

	// Check for --mfa-method flag override
	if MFAMethodFlag != "" {
		var overrideMethod *MFAMethodResponse
		for _, method := range mfaStatus.Methods {
			if method.Type == MFAMethodFlag && !method.IsEnrolling {
				overrideMethod = &method
				break
			}
		}
		if overrideMethod == nil {
			return "", fmt.Errorf("MFA method %q not found or not enrolled", MFAMethodFlag)
		}
		return collectMFAAuth(cmd, baseURL, bearer, *overrideMethod, mfaStatus.Methods)
	}

	// Use server's preferred method if set
	var preferredMethod *MFAMethodResponse
	if mfaStatus.PreferredMethod != nil && *mfaStatus.PreferredMethod != "" {
		for _, method := range mfaStatus.Methods {
			if method.Type == *mfaStatus.PreferredMethod && !method.IsEnrolling {
				preferredMethod = &method
				break
			}
		}
	}

	if preferredMethod != nil {
		return collectMFAAuth(cmd, baseURL, bearer, *preferredMethod, mfaStatus.Methods)
	}

	// Fall back to CLI preference
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

	// Try methods based on preference
	methods := filterMethodsByPreference(mfaStatus.Methods, preference)
	if len(methods) == 0 {
		return "", fmt.Errorf("no MFA methods available (preference: %q)", preference)
	}

	// Use the first preferred method
	return collectMFAAuth(cmd, baseURL, bearer, methods[0], mfaStatus.Methods)
}

// collectMFAAuth collects MFA verification and returns the auth string
// Returns format: "totp:123456" or "webauthn:assertion_json"
func collectMFAAuth(cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, allMethods []MFAMethodResponse) (string, error) {
	out := cmd.OutOrStdout()

	switch method.Type {
	case "webauthn":
		if err := writeLine(out, "Multi-factor authentication required (WebAuthn)."); err != nil {
			return "", err
		}

		// Perform WebAuthn and get assertion data
		assertionData, err := collectWebAuthnAssertion(baseURL, bearer)
		if err != nil {
			return "", fmt.Errorf("WebAuthn verification failed: %w", err)
		}

		return "webauthn:" + assertionData, nil

	case "totp":
		if err := writeLine(out, "Multi-factor authentication required (TOTP)."); err != nil {
			return "", err
		}

		// Prompt for TOTP code
		code, err := promptMFACode()
		if err != nil {
			return "", err
		}

		return "totp:" + code, nil

	default:
		return "", fmt.Errorf("unsupported MFA method for inline verification: %s", method.Type)
	}
}

// collectWebAuthnAssertion performs WebAuthn and returns the assertion as a JSON string
func collectWebAuthnAssertion(baseURL, bearer string) (string, error) {
	// This is similar to tryWebAuthnVerification but returns the assertion instead of verifying it
	// The gateway will verify it when we pass it in the auth string
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/webauthn/assertion/start", baseURL)
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to start WebAuthn assertion")
	}

	var startResp struct {
		Options struct {
			PublicKey struct {
				Challenge        string `json:"challenge"`
				RPID             string `json:"rpId"`
				AllowCredentials []struct {
					ID string `json:"id"`
				} `json:"allowCredentials"`
				Timeout          int    `json:"timeout"`
				UserVerification string `json:"userVerification"`
			} `json:"publicKey"`
		} `json:"options"`
		SessionData string `json:"session_data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return "", err
	}

	// Extract credential IDs
	var allowedCreds []string
	for _, cred := range startResp.Options.PublicKey.AllowCredentials {
		allowedCreds = append(allowedCreds, cred.ID)
	}

	// Perform WebAuthn assertion
	assertion, err := webauthn.GetAssertion(webauthn.AssertionOptions{
		Challenge:        startResp.Options.PublicKey.Challenge,
		RPID:             startResp.Options.PublicKey.RPID,
		AllowCredentials: allowedCreds,
		Timeout:          startResp.Options.PublicKey.Timeout,
		UserVerification: startResp.Options.PublicKey.UserVerification,
		Origin:           baseURL,
	})
	if err != nil {
		return "", err
	}

	// Create assertion response with session data
	assertionData := map[string]interface{}{
		"session_data": startResp.SessionData,
		"assertion": map[string]string{
			"credential_id":      assertion.CredentialID,
			"authenticator_data": assertion.AuthenticatorData,
			"client_data_json":   assertion.ClientDataJSON,
			"signature":          assertion.Signature,
			"user_handle":        assertion.UserHandle,
		},
	}

	// Encode to JSON and then base64
	jsonData, err := json.Marshal(assertionData)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}
