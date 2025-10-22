package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/spf13/cobra"
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

	return "", nil
}

func promptMFAForCommand(cmd *cobra.Command, baseURL, bearer, rack string) (string, error) {
	if MFACodeFlag != "" {
		code := strings.TrimSpace(MFACodeFlag)
		if code != "" {
			return "totp." + code, nil
		}
	}

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

	methods := filterMethodsByPreference(mfaStatus.Methods, preference)
	if len(methods) == 0 {
		return "", fmt.Errorf("no MFA methods available (preference: %q)", preference)
	}

	return collectMFAAuth(cmd, baseURL, bearer, rack, methods[0], mfaStatus.Methods)
}

func collectMFAAuth(cmd *cobra.Command, baseURL, bearer, rack string, method MFAMethodResponse, allMethods []MFAMethodResponse) (string, error) {
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
	endpoint := fmt.Sprintf("%s/api/v1/auth/mfa/webauthn/assertion/start", baseURL)
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

	if resp.StatusCode != http.StatusOK {
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

	var allowedCreds []string
	for _, cred := range startResp.Options.PublicKey.AllowCredentials {
		allowedCreds = append(allowedCreds, cred.ID)
	}

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

	assertionJSON, err := json.Marshal(webauthnResponse)
	if err != nil {
		return "", err
	}

	inlineData := map[string]any{
		"session_data":       startResp.SessionData,
		"assertion_response": string(assertionJSON),
	}

	jsonData, err := json.Marshal(inlineData)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}
