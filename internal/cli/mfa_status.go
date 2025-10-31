package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func getMFAStatus(baseURL, sessionToken string) (*MFAStatusResponse, error) {
	endpoint := fmt.Sprintf("%s/api/v1/auth/mfa/status", strings.TrimSuffix(baseURL, "/"))
	resp, err := authorizedGatewayRequest(http.MethodGet, endpoint, sessionToken, nil, "")
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

func filterMethodsByPreference(methods []MFAMethodResponse, preference string) []MFAMethodResponse {
	priorities := []string{preference}
	if preference == "" || preference == "default" {
		priorities = []string{"webauthn", "totp", "backup_code"}
	}
	return filterMethodsByPriority(methods, priorities)
}

func filterMethodsByPriority(methods []MFAMethodResponse, priorities []string) []MFAMethodResponse {
	var ordered []MFAMethodResponse
	for _, priority := range priorities {
		for _, method := range methods {
			if !method.IsEnrolling && method.Type == priority {
				ordered = append(ordered, method)
			}
		}
	}
	return ordered
}
