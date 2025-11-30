package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func checkMFAAndGetAuth(cmd *cobra.Command, commandName string) (string, error) {
	if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
		return "", nil
	}

	convoxCmd, ok := convox.LookupCommand(commandName)
	if !ok {
		return "", nil
	}

	rack, err := SelectedRack()
	if err != nil {
		return "", err
	}

	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", err
	}

	mfaLevel := rbac.GetMFALevel(convoxCmd.Permissions)
	if mfaLevel == rbac.MFANone {
		return "", nil
	}

	if mfaLevel == rbac.MFAAlways {
		return promptMFAForCommand(cmd, gatewayURL, bearer, rack)
	}

	// MFAStepUp: Check if step-up window is fresh
	if mfaLevel == rbac.MFAStepUp {
		return promptMFAIfStepUpExpired(cmd, gatewayURL, bearer, rack)
	}

	return "", nil
}

func promptMFAIfStepUpExpired(cmd *cobra.Command, baseURL, bearer, rack string) (string, error) {
	// Allow override via --mfa-code flag
	if code, ok := inlineTotpFromFlag(); ok {
		return code, nil
	}

	// Check current MFA status including step-up expiration
	status, err := getMFAStatus(baseURL, bearer)
	if err != nil {
		return "", fmt.Errorf("failed to check MFA status: %w", err)
	}

	// Check if step-up is fresh
	if isStepUpFresh(status.RecentStepUpExpiresAt, time.Now()) {
		return "", nil // Step-up is fresh, no need to prompt
	}

	// Step-up expired or never set - prompt for MFA
	if !status.Enrolled {
		return "", fmt.Errorf("MFA not enrolled - this command requires MFA")
	}
	if len(status.Methods) == 0 {
		if status.Enrolled {
			return "", fmt.Errorf(
				"no CLI-compatible MFA methods found. " +
					"Edit your WebAuthn methods in the web UI and enable 'CLI Compatible'",
			)
		}
		return "", fmt.Errorf("no MFA methods enrolled")
	}

	method, err := selectMFAMethod(status, rack)
	if err != nil {
		return "", err
	}

	return collectMFAAuth(cmd, baseURL, bearer, rack, method, status.Methods)
}

// isStepUpFresh checks if the step-up window is still valid with a safety buffer.
// Returns true if the expiration time is at least 10 seconds in the future.
func isStepUpFresh(expiresAt *time.Time, now time.Time) bool {
	const stepUpSafetyBuffer = 10 * time.Second

	if expiresAt == nil {
		return false
	}

	expiresIn := expiresAt.Sub(now)
	return expiresIn > stepUpSafetyBuffer
}

func promptMFAForCommand(cmd *cobra.Command, baseURL, bearer, rack string) (string, error) {
	if code, ok := inlineTotpFromFlag(); ok {
		return code, nil
	}

	status, err := loadMFAStatus(baseURL, bearer)
	if err != nil {
		return "", err
	}

	method, err := selectMFAMethod(status, rack)
	if err != nil {
		return "", err
	}

	return collectMFAAuth(cmd, baseURL, bearer, rack, method, status.Methods)
}

func inlineTotpFromFlag() (string, bool) {
	code := strings.TrimSpace(MFACodeFlag)
	if code == "" {
		return "", false
	}
	return "totp." + code, true
}

func loadMFAStatus(baseURL, bearer string) (*MFAStatusResponse, error) {
	status, err := getMFAStatus(baseURL, bearer)
	if err != nil {
		return nil, fmt.Errorf("failed to check MFA status: %w", err)
	}
	if !status.Enrolled {
		return nil, fmt.Errorf("MFA not enrolled - this command requires MFA")
	}
	if len(status.Methods) == 0 {
		if status.Enrolled {
			return nil, fmt.Errorf(
				"no CLI-compatible MFA methods found. " +
					"Edit your WebAuthn methods in the web UI and enable 'CLI Compatible'",
			)
		}
		return nil, fmt.Errorf("no MFA methods enrolled")
	}
	return status, nil
}

func selectMFAMethod(status *MFAStatusResponse, rack string) (MFAMethodResponse, error) {
	if MFAMethodFlag != "" {
		method, ok := overrideMFAMethod(status.Methods)
		if !ok {
			return MFAMethodResponse{}, fmt.Errorf("MFA method %q not found or not enrolled", MFAMethodFlag)
		}
		return method, nil
	}

	if method, ok := preferredMFAMethod(status); ok {
		return method, nil
	}

	preference := resolveMFAPreference(rack)
	methods := filterMethodsByPreference(status.Methods, preference)
	if len(methods) == 0 {
		return MFAMethodResponse{}, fmt.Errorf("no MFA methods available (preference: %q)", preference)
	}
	return methods[0], nil
}

func overrideMFAMethod(methods []MFAMethodResponse) (MFAMethodResponse, bool) {
	if MFAMethodFlag == "" {
		return MFAMethodResponse{}, false
	}
	for _, method := range methods {
		if method.Type == MFAMethodFlag && !method.IsEnrolling {
			return method, true
		}
	}
	return MFAMethodResponse{}, false
}

func preferredMFAMethod(status *MFAStatusResponse) (MFAMethodResponse, bool) {
	if status.PreferredMethod == nil || *status.PreferredMethod == "" {
		return MFAMethodResponse{}, false
	}
	for _, method := range status.Methods {
		if method.Type == *status.PreferredMethod && !method.IsEnrolling {
			return method, true
		}
	}
	return MFAMethodResponse{}, false
}

func collectMFAAuth(
	cmd *cobra.Command,
	baseURL, bearer, _ string,
	method MFAMethodResponse,
	_ []MFAMethodResponse,
) (string, error) {
	out := cmd.ErrOrStderr()

	switch method.Type {
	case "webauthn":
		if err := writeLine(out, "Multi-factor authentication required (WebAuthn)."); err != nil {
			return "", err
		}

		assertionData, err := collectWebAuthnAssertion(baseURL, bearer)
		if err != nil {
			return "", fmt.Errorf("WebAuthn verification failed: %w", err)
		}

		return "webauthn." + assertionData, nil

	case "totp":
		if err := writeLine(out, "Multi-factor authentication required (TOTP)."); err != nil {
			return "", err
		}

		code, err := promptMFACode()
		if err != nil {
			return "", err
		}

		return "totp." + code, nil

	default:
		return "", fmt.Errorf("unsupported MFA method for inline verification: %s", method.Type)
	}
}

func collectWebAuthnAssertion(baseURL, bearer string) (string, error) {
	result, _, err := collectWebAuthnAssertionWithPIN(baseURL, bearer, "")
	return result, err
}

// collectWebAuthnAssertionWithPIN collects a WebAuthn assertion, optionally using a cached PIN.
// Returns the assertion data, the PIN used (for caching), and any error.
func collectWebAuthnAssertionWithPIN(baseURL, bearer, cachedPIN string) (string, string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/auth/mfa/webauthn/assertion/start", baseURL)
	req, err := http.NewRequest(http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to start WebAuthn assertion")
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
		return "", "", err
	}

	allowedCreds := make([]string, 0, len(startResp.Options.PublicKey.AllowCredentials))
	for _, cred := range startResp.Options.PublicKey.AllowCredentials {
		allowedCreds = append(allowedCreds, cred.ID)
	}

	assertion, pinUsed, err := webauthn.GetAssertionWithCachedPIN(webauthn.AssertionOptions{
		Challenge:        startResp.Options.PublicKey.Challenge,
		RPID:             startResp.Options.PublicKey.RPID,
		AllowCredentials: allowedCreds,
		Timeout:          startResp.Options.PublicKey.Timeout,
		UserVerification: startResp.Options.PublicKey.UserVerification,
		Origin:           baseURL,
	}, cachedPIN)
	if err != nil {
		return "", "", err
	}

	assertionJSON, err := marshalWebAuthnResponse(assertion)
	if err != nil {
		return "", "", err
	}

	inlineData := map[string]any{
		"session_data":       startResp.SessionData,
		"assertion_response": assertionJSON,
	}

	jsonData, err := json.Marshal(inlineData)
	if err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(jsonData), pinUsed, nil
}

func marshalWebAuthnResponse(assertion *webauthn.AssertionResponse) (string, error) {
	response := map[string]any{
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

	payload, err := json.Marshal(response)
	if err != nil {
		return "", err
	}

	return string(payload), nil
}

func resolveMFAPreference(rack string) string {
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

	return preference
}
