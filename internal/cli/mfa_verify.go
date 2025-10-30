package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
)

// verifyMFAMethod handles verification for a single MFA method.
func verifyMFAMethod(cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, allMethods []MFAMethodResponse) error {
	out := cmd.ErrOrStderr()

	switch method.Type {
	case "webauthn":
		return handleWebAuthnVerification(out, baseURL, bearer, allMethods)
	case "totp":
		return handleTOTPVerification(out, baseURL, bearer, allMethods)
	case "backup_code":
		return handleBackupCodeVerification(out, baseURL, bearer)
	default:
		return fmt.Errorf("unsupported MFA method: %s", method.Type)
	}
}

func handleWebAuthnVerification(out io.Writer, baseURL, bearer string, methods []MFAMethodResponse) error {
	if hasMethodType(methods, "totp") {
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
	return writeLine(out, "MFA verified.")
}

func handleTOTPVerification(out io.Writer, baseURL, bearer string, methods []MFAMethodResponse) error {
	if hasMethodType(methods, "webauthn") {
		if err := writeLine(out, "Tip: Pass `--mfa-method webauthn` to use your security key instead."); err != nil {
			return err
		}
	}
	if err := writeLine(out, "Multi-factor authentication required."); err != nil {
		return err
	}
	return attemptTOTPVerification(out, baseURL, bearer)
}

func attemptTOTPVerification(out io.Writer, baseURL, bearer string) error {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := promptMFACode()
		if err != nil {
			return err
		}
		if strings.TrimSpace(code) == "" {
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
		return writeLine(out, "MFA verified.")
	}
	return fmt.Errorf("failed to verify MFA after multiple attempts")
}

func handleBackupCodeVerification(out io.Writer, baseURL, bearer string) error {
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
	return writeLine(out, "MFA verified.")
}

// tryWebAuthnVerification performs WebAuthn assertion flow.
func tryWebAuthnVerification(baseURL, sessionToken string) error {
	start, err := startWebAuthnAssertion(baseURL, sessionToken)
	if err != nil {
		return err
	}

	allowedCreds := extractAllowedCredentialIDs(start)
	printAllowedCredentials(allowedCreds)
	if len(allowedCreds) == 0 {
		return fmt.Errorf("no WebAuthn credentials found - please enroll a security key first")
	}

	options, err := buildAssertionOptions(baseURL, allowedCreds, start)
	if err != nil {
		return err
	}

	assertion, err := webauthn.GetAssertion(options)
	if err != nil {
		return fmt.Errorf("WebAuthn assertion failed: %w", err)
	}

	assertionJSON, err := marshalWebAuthnResponse(assertion)
	if err != nil {
		return fmt.Errorf("failed to marshal assertion: %w", err)
	}

	return submitWebAuthnAssertion(baseURL, sessionToken, start.SessionData, assertionJSON)
}

type webAuthnStartResponse struct {
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

func startWebAuthnAssertion(baseURL, sessionToken string) (*webAuthnStartResponse, error) {
	endpoint := fmt.Sprintf("%s/api/v1/auth/mfa/webauthn/assertion/start", strings.TrimSuffix(baseURL, "/"))
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to start assertion: %s", RenderGatewayError(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var start webAuthnStartResponse
	if err := json.Unmarshal(bodyBytes, &start); err != nil {
		return nil, fmt.Errorf("failed to decode assertion start response: %w", err)
	}

	return &start, nil
}

func extractAllowedCredentialIDs(start *webAuthnStartResponse) []string {
	ids := make([]string, 0, len(start.Options.PublicKey.AllowCredentials))
	for _, cred := range start.Options.PublicKey.AllowCredentials {
		ids = append(ids, cred.ID)
	}
	return ids
}

func printAllowedCredentials(creds []string) {
	for i, cred := range creds {
		if len(cred) > 40 {
			fmt.Fprintf(os.Stderr, "    [%d] %s...\n", i+1, cred[:40])
		} else {
			fmt.Fprintf(os.Stderr, "    [%d] %s\n", i+1, cred)
		}
	}
}

func buildAssertionOptions(baseURL string, allowedCreds []string, start *webAuthnStartResponse) (webauthn.AssertionOptions, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return webauthn.AssertionOptions{}, fmt.Errorf("failed to parse gateway URL: %w", err)
	}

	origin := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	rpID := parsedURL.Hostname()

	return webauthn.AssertionOptions{
		Challenge:        start.Options.PublicKey.Challenge,
		RPID:             rpID,
		AllowCredentials: allowedCreds,
		Timeout:          start.Options.PublicKey.Timeout,
		UserVerification: start.Options.PublicKey.UserVerification,
		Origin:           origin,
	}, nil
}

func submitWebAuthnAssertion(baseURL, sessionToken, sessionData, assertionJSON string) error {
	verifyEndpoint := fmt.Sprintf("%s/api/v1/auth/mfa/webauthn/assertion/verify", strings.TrimSuffix(baseURL, "/"))
	payload := map[string]any{
		"session_data":       sessionData,
		"assertion_response": assertionJSON,
		"trust_device":       false,
	}
	verifyBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal verify payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, verifyEndpoint, bytes.NewReader(verifyBody))
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
		return fmt.Errorf("assertion verification failed: %s", RenderGatewayError(bodyBytes))
	}

	return nil
}

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

func submitMFAVerification(baseURL, sessionToken, code string) error {
	endpoint := fmt.Sprintf("%s/api/v1/auth/mfa/verify", strings.TrimSuffix(baseURL, "/"))
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

func checkWebAuthnAvailability() bool {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()
	return webauthn.CheckAvailability()
}

func hasMethodType(methods []MFAMethodResponse, methodType string) bool {
	for _, m := range methods {
		if m.Type == methodType && !m.IsEnrolling {
			return true
		}
	}
	return false
}
