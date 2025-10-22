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

func filterMethodsByPreference(methods []MFAMethodResponse, preference string) []MFAMethodResponse {
	if preference == "default" {
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

	var filtered []MFAMethodResponse
	for _, m := range methods {
		if m.Type == preference && !m.IsEnrolling {
			filtered = append(filtered, m)
		}
	}
	return filtered
}
