package cli

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// verifyMFAMethod handles verification for a single MFA method
// It outputs appropriate prompts and handles WebAuthn and TOTP flows
func verifyMFAMethod(cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, allMethods []MFAMethodResponse) error {
	// Use stderr for all prompts so stdout can be used for --output token
	out := cmd.ErrOrStderr()

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

// getMFAStatus fetches MFA status from the gateway API
func getMFAStatus(baseURL, sessionToken string) (*MFAStatusResponse, error) {
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/status", strings.TrimSuffix(baseURL, "/"))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway error: %s", RenderGatewayError(bodyBytes))
	}

	var status MFAStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode MFA status: %w", err)
	}

	return &status, nil
}

// filterMethodsByPreference returns methods sorted by preference
func filterMethodsByPreference(methods []MFAMethodResponse, preference string) []MFAMethodResponse {
	if preference == "default" {
		// Default mode: WebAuthn first, then TOTP, then backup codes
		var ordered []MFAMethodResponse
		for _, m := range methods {
			if m.Type == "webauthn" && !m.IsEnrolling {
				ordered = append(ordered, m)
			}
		}
		for _, m := range methods {
			if m.Type == "totp" && !m.IsEnrolling {
				ordered = append(ordered, m)
			}
		}
		for _, m := range methods {
			if m.Type == "backup_code" && !m.IsEnrolling {
				ordered = append(ordered, m)
			}
		}
		return ordered
	}

	// Specific preference - filter to only that type
	var filtered []MFAMethodResponse
	for _, m := range methods {
		if m.Type == preference && !m.IsEnrolling {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// tryWebAuthnVerification performs WebAuthn assertion flow
func tryWebAuthnVerification(baseURL, sessionToken string) error {
	// Get assertion challenge from gateway
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/webauthn/assertion/start", strings.TrimSuffix(baseURL, "/"))
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to start assertion: %s", RenderGatewayError(bodyBytes))
	}

	var startResp struct {
		Options struct {
			PublicKey struct {
				Challenge        string `json:"challenge"`
				Timeout          int    `json:"timeout"`
				RPID             string `json:"rpId"`
				AllowCredentials []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"allowCredentials"`
				UserVerification string `json:"userVerification"`
			} `json:"publicKey"`
		} `json:"options"`
		SessionData string `json:"session_data"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &startResp); err != nil {
		return fmt.Errorf("failed to decode assertion start response: %w", err)
	}

	// Extract allowed credential IDs
	var allowedCreds []string
	for _, cred := range startResp.Options.PublicKey.AllowCredentials {
		allowedCreds = append(allowedCreds, cred.ID)
	}

	// Get origin and RPID from base URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse gateway URL: %w", err)
	}
	origin := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	rpID := parsedURL.Hostname()

	// Debug output for credential info
	for i, cred := range allowedCreds {
		if len(cred) > 40 {
			fmt.Fprintf(os.Stderr, "    [%d] %s...\n", i+1, cred[:40])
		} else {
			fmt.Fprintf(os.Stderr, "    [%d] %s\n", i+1, cred)
		}
	}

	// Check if we have any credentials
	if len(allowedCreds) == 0 {
		return fmt.Errorf("no WebAuthn credentials found - please enroll a security key first")
	}

	// Perform WebAuthn assertion
	assertionOpts := webauthn.AssertionOptions{
		Challenge:        startResp.Options.PublicKey.Challenge,
		RPID:             rpID,
		AllowCredentials: allowedCreds,
		Timeout:          startResp.Options.PublicKey.Timeout,
		UserVerification: startResp.Options.PublicKey.UserVerification,
		Origin:           origin,
	}

	assertion, err := webauthn.GetAssertion(assertionOpts)
	if err != nil {
		return fmt.Errorf("WebAuthn assertion failed: %w", err)
	}

	// Format assertion response in WebAuthn spec format
	webauthnResponse := map[string]any{
		"id":    assertion.CredentialID,
		"rawId": assertion.CredentialID,
		"response": map[string]string{
			"authenticatorData": assertion.AuthenticatorData,
			"clientDataJSON":    assertion.ClientDataJSON,
			"signature":         assertion.Signature,
			"userHandle":        assertion.UserHandle,
		},
		"type": "public-key",
	}

	// Serialize assertion response
	assertionJSON, err := json.Marshal(webauthnResponse)
	if err != nil {
		return fmt.Errorf("failed to marshal assertion: %w", err)
	}

	// Submit assertion to gateway
	verifyEndpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/webauthn/assertion/verify", strings.TrimSuffix(baseURL, "/"))
	verifyPayload := map[string]any{
		"session_data":       startResp.SessionData,
		"assertion_response": string(assertionJSON),
		"trust_device":       false,
	}

	verifyBody, err := json.Marshal(verifyPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal verify payload: %w", err)
	}

	verifyReq, err := http.NewRequest(http.MethodPost, verifyEndpoint, bytes.NewReader(verifyBody))
	if err != nil {
		return err
	}
	verifyReq.Header.Set("Authorization", "Bearer "+sessionToken)
	verifyReq.Header.Set("Content-Type", "application/json")

	verifyResp, err := HTTPClient.Do(verifyReq)
	if err != nil {
		return err
	}
	defer func() { _ = verifyResp.Body.Close() }()

	if verifyResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(verifyResp.Body)
		return fmt.Errorf("assertion verification failed: %s", RenderGatewayError(bodyBytes))
	}

	return nil
}

// promptMFACode prompts the user to enter an MFA code
func promptMFACode() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Enter MFA code (TOTP or backup code): ")
		codeBytes, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(codeBytes)), nil
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "Enter MFA code (TOTP, Yubikey OTP, or backup code): ")
	code, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(code), nil
}

// submitMFAVerification submits an MFA code for verification
func submitMFAVerification(baseURL, sessionToken, code string) error {
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/verify", strings.TrimSuffix(baseURL, "/"))
	payload := map[string]any{
		"code": code,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("gateway error: %s", RenderGatewayError(bodyBytes))
}

// checkWebAuthnAvailability safely checks if WebAuthn is available
func checkWebAuthnAvailability() bool {
	defer func() {
		if r := recover(); r != nil {
			// Device check panicked, assume not available
			_ = r
		}
	}()
	return webauthn.CheckAvailability()
}

// hasMethodType checks if any method in the list has the given type
func hasMethodType(methods []MFAMethodResponse, methodType string) bool {
	for _, m := range methods {
		if m.Type == methodType && !m.IsEnrolling {
			return true
		}
	}
	return false
}

// ============================================================================
// MFA Requirement Checking for Convox Commands
// ============================================================================

// checkMFAAndGetAuth checks if the command requires MFA and returns the auth string
// Returns: mfaAuth string in format "totp.code" or "webauthn.assertion" or "" for no MFA
func checkMFAAndGetAuth(cmd *cobra.Command, commandName string) (string, error) {
	// API tokens bypass all MFA checks (for CI/CD automation)
	if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
		return "", nil
	}

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

	// Check if MFA is required for this command
	mfaLevel := convox.GetMFALevel(convoxCmd.Permissions)
	if mfaLevel == convox.MFANone {
		// No MFA required
		return "", nil
	}

	// For MFAAlways: Always prompt upfront and return inline MFA auth
	if mfaLevel == convox.MFAAlways {
		return promptMFAForCommand(cmd, gatewayURL, bearer, rack)
	}

	// For MFAStepUp: Don't prompt - let the server tell us if MFA is needed
	// The Convox request handler will retry with MFA if server returns mfa_required
	return "", nil
}

// promptMFAForCommand prompts the user for MFA and returns the auth string
// Returns format: "totp.123456" or "webauthn.assertion_data"
func promptMFAForCommand(cmd *cobra.Command, baseURL, bearer, rack string) (string, error) {
	// Check for --mfa-code flag first (TOTP/Yubikey/backup code)
	if MFACodeFlag != "" {
		code := strings.TrimSpace(MFACodeFlag)
		if code != "" {
			return "totp." + code, nil
		}
	}

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
		return collectMFAAuth(cmd, baseURL, bearer, rack, *overrideMethod, mfaStatus.Methods)
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
		return collectMFAAuth(cmd, baseURL, bearer, rack, *preferredMethod, mfaStatus.Methods)
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
	return collectMFAAuth(cmd, baseURL, bearer, rack, methods[0], mfaStatus.Methods)
}

// collectMFAAuth collects MFA verification and returns the auth string for inline use
// Returns format: "totp.123456" or "webauthn.assertion_json"
func collectMFAAuth(cmd *cobra.Command, baseURL, bearer, rack string, method MFAMethodResponse, allMethods []MFAMethodResponse) (string, error) {
	// Use stderr for all prompts so stdout can be used for --output token
	out := cmd.ErrOrStderr()

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

		return "webauthn." + assertionData, nil

	case "totp":
		if err := writeLine(out, "Multi-factor authentication required (TOTP)."); err != nil {
			return "", err
		}

		// Prompt for TOTP code
		code, err := promptMFACode()
		if err != nil {
			return "", err
		}

		return "totp." + code, nil

	default:
		return "", fmt.Errorf("unsupported MFA method for inline verification: %s", method.Type)
	}
}

// collectWebAuthnAssertion performs WebAuthn and returns the assertion as a base64-encoded JSON string
func collectWebAuthnAssertion(baseURL, bearer string) (string, error) {
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

	// Format assertion response in WebAuthn spec format (same as tryWebAuthnVerification)
	webauthnResponse := map[string]any{
		"id":    assertion.CredentialID,
		"rawId": assertion.CredentialID,
		"response": map[string]string{
			"authenticatorData": assertion.AuthenticatorData,
			"clientDataJSON":    assertion.ClientDataJSON,
			"signature":         assertion.Signature,
			"userHandle":        assertion.UserHandle,
		},
		"type": "public-key",
	}

	// Serialize assertion response
	assertionJSON, err := json.Marshal(webauthnResponse)
	if err != nil {
		return "", err
	}

	// Create inline data with session_data and assertion_response
	inlineData := map[string]interface{}{
		"session_data":       startResp.SessionData,
		"assertion_response": string(assertionJSON),
	}

	// Encode to JSON and then base64
	jsonData, err := json.Marshal(inlineData)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}
