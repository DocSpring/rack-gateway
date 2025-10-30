package cli

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// SelectedRack determines which rack to use based on flags, env vars, or config
func SelectedRack() (string, error) {
	if RackFlag != "" {
		return RackFlag, nil
	}
	if env := strings.TrimSpace(os.Getenv("RACK_GATEWAY_RACK")); env != "" {
		return env, nil
	}
	if url := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL")); url != "" {
		if label := strings.TrimSpace(os.Getenv("RACK_GATEWAY_RACK")); label != "" {
			return label, nil
		}
		return "(from environment)", nil
	}
	rack, err := GetCurrentRack()
	if err != nil {
		return "", fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url> or use --rack flag")
	}
	if strings.TrimSpace(rack) == "" {
		return "", fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url> or use --rack flag")
	}
	return rack, nil
}

// NormalizeGatewayURL ensures the gateway URL has a proper scheme and no trailing slash
func NormalizeGatewayURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("invalid gateway url")
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + trimmed
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	return trimmed, nil
}

// ResolveRackStatus returns detailed status information about the current rack
func ResolveRackStatus(now time.Time) (*RackStatus, error) {
	if status, err := resolveStatusFromConfig(now); status != nil || err != nil {
		return status, err
	}
	return resolveStatusFromEnv()
}

func resolveStatusFromConfig(now time.Time) (*RackStatus, error) {
	cfg, exists, err := LoadConfig()
	if err != nil || !exists {
		return nil, err
	}

	rack := strings.TrimSpace(cfg.Current)
	if rack == "" {
		return nil, nil
	}

	gateway, ok := cfg.Gateways[rack]
	if !ok {
		return nil, fmt.Errorf("rack %s not configured", rack)
	}

	status := &RackStatus{Rack: rack, GatewayURL: gateway.URL}
	return populateStatusLines(status, gateway, now), nil
}

func populateStatusLines(status *RackStatus, gateway GatewayConfig, now time.Time) *RackStatus {
	switch {
	case gateway.Token == "":
		status.StatusLines = append(status.StatusLines, "Status: Not logged in")
	case now.After(gateway.ExpiresAt):
		status.StatusLines = append(status.StatusLines, "Status: Token expired")
	default:
		status.StatusLines = append(status.StatusLines,
			fmt.Sprintf("Status: Logged in as %s", gateway.Email))
		status.StatusLines = append(status.StatusLines,
			fmt.Sprintf("Token expires: %s", gateway.ExpiresAt.Format(time.RFC3339)))
		if !gateway.MFAVerified {
			status.StatusLines = append(status.StatusLines, "MFA: verification required (run an interactive login)")
		}
	}
	return status
}

func resolveStatusFromEnv() (*RackStatus, error) {
	envURL := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL"))
	if envURL == "" {
		return nil, fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url>")
	}

	label := strings.TrimSpace(os.Getenv("RACK_GATEWAY_RACK"))
	tokenEnv := strings.TrimSpace(os.Getenv("RACK_GATEWAY_API_TOKEN"))
	if label == "" {
		if tokenEnv == "" {
			return nil, fmt.Errorf("RACK_GATEWAY_API_TOKEN must be set when relying on RACK_GATEWAY_URL without a rack name")
		}
		label = "Using RACK_GATEWAY_API_TOKEN from environment"
	}

	status := &RackStatus{Rack: label, GatewayURL: envURL}
	if tokenEnv == "" {
		status.StatusLines = append(status.StatusLines, "Status: RACK_GATEWAY_API_TOKEN not set in environment")
	}

	return status, nil
}
