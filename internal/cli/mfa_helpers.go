package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// performMFAStepUp handles MFA verification for step-up authentication scenarios
// (e.g., deploy approvals, sensitive operations). It respects --mfa-code and --mfa-method flags,
// server preferences, and CLI preferences.
func performMFAStepUp(cmd *cobra.Command, baseURL, bearer, rack string) error {
	// If --mfa-code flag provided, submit it directly (for TOTP/Yubikey/backup codes)
	if MFACodeFlag != "" {
		code := strings.TrimSpace(MFACodeFlag)
		if code != "" {
			if err := submitMFAVerification(baseURL, bearer, code); err != nil {
				return fmt.Errorf("MFA verification failed: %w", err)
			}
			return nil
		}
	}

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
	if MFAMethodFlag != "" {
		var overrideMethod *MFAMethodResponse
		for _, method := range mfaStatus.Methods {
			if method.Type == MFAMethodFlag && !method.IsEnrolling {
				overrideMethod = &method
				break
			}
		}
		if overrideMethod == nil {
			return fmt.Errorf("MFA method %q not found or not enrolled", MFAMethodFlag)
		}
		return verifyMFAMethod(cmd, baseURL, bearer, *overrideMethod, mfaStatus.Methods)
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

	// If server has a preferred method, use it
	if preferredMethod != nil {
		return verifyMFAMethod(cmd, baseURL, bearer, *preferredMethod, mfaStatus.Methods)
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
func verifyMFAMethod(cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, allMethods []MFAMethodResponse) error {
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
			fmt.Printf("    [%d] %s...\n", i+1, cred[:40])
		} else {
			fmt.Printf("    [%d] %s\n", i+1, cred)
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
		fmt.Print("Enter MFA code (TOTP or backup code): ")
		codeBytes, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(codeBytes)), nil
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter MFA code (TOTP, Yubikey OTP, or backup code): ")
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
