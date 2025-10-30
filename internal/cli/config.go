package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func configFile() string {
	return filepath.Join(ConfigPath, "config.json")
}

// LoadConfig loads the CLI configuration from disk
func LoadConfig() (*Config, bool, error) {
	cfg := &Config{}
	path := configFile()
	data, err := os.ReadFile(path)
	exists := true
	if err != nil {
		if os.IsNotExist(err) {
			exists = false
		} else {
			return nil, false, err
		}
	} else {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, false, err
		}
	}
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	dirty := false
	if strings.TrimSpace(cfg.MachineID) == "" {
		cfg.MachineID = uuid.NewString()
		dirty = true
	}
	if cfg.MFAPreference == "" {
		cfg.MFAPreference = "default" // Default to user's profile preference
		dirty = true
	}
	if dirty {
		if err := SaveConfig(cfg); err != nil {
			return nil, exists, err
		}
	}
	return cfg, exists, nil
}

// SaveConfig saves the CLI configuration to disk
func SaveConfig(cfg *Config) error {
	if err := os.MkdirAll(ConfigPath, 0o700); err != nil {
		return err
	}
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configFile(), data, 0o600); err != nil {
		return err
	}
	// Remove standalone current file if it exists so config.json remains the source of truth.
	_ = os.Remove(filepath.Join(ConfigPath, "current"))
	return nil
}

// SaveToken saves authentication token for a rack
func SaveToken(rack string, loginResp *LoginResponse) error {
	cfg, _, err := LoadConfig()
	if err != nil {
		return err
	}
	gateway, ok := cfg.Gateways[rack]
	if !ok {
		return fmt.Errorf("gateway not configured for rack: %s", rack)
	}
	gateway.Token = loginResp.Token
	gateway.Email = loginResp.Email
	gateway.ExpiresAt = loginResp.ExpiresAt
	gateway.SessionID = loginResp.SessionID
	gateway.Channel = loginResp.Channel
	gateway.DeviceID = loginResp.DeviceID
	gateway.DeviceName = loginResp.DeviceName
	gateway.MFAVerified = loginResp.MFAVerified
	cfg.Gateways[rack] = gateway
	return SaveConfig(cfg)
}

// LoadToken loads the authentication token for a rack
func LoadToken(rack string) (*GatewayConfig, error) {
	cfg, exists, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("no configuration found")
	}
	gateway, ok := cfg.Gateways[rack]
	if !ok {
		return nil, fmt.Errorf("no gateway found for rack: %s", rack)
	}
	if gateway.Token == "" {
		return nil, fmt.Errorf("no token found for rack: %s", rack)
	}

	if time.Now().After(gateway.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return &gateway, nil
}

// SaveGatewayConfig saves the gateway URL for a rack
func SaveGatewayConfig(rack, gatewayURL string) error {
	cfg, _, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.Gateways[rack] = GatewayConfig{URL: gatewayURL}
	return SaveConfig(cfg)
}

// LoadGatewayURL loads the gateway URL for a rack
func LoadGatewayURL(rack string) (string, error) {
	cfg, exists, err := LoadConfig()
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("no configuration found")
	}
	gateway, ok := cfg.Gateways[rack]
	if !ok {
		return "", fmt.Errorf("no gateway configured for rack: %s", rack)
	}

	return gateway.URL, nil
}

// LoadRackAuth loads the gateway URL and token for a rack
func LoadRackAuth(rack string) (string, string, error) {
	gatewayURL := os.Getenv("RACK_GATEWAY_URL")
	if gatewayURL == "" {
		var err error
		gatewayURL, err = LoadGatewayURL(rack)
		if err != nil {
			return "", "", err
		}
	}

	normalized, err := NormalizeGatewayURL(gatewayURL)
	if err != nil {
		return "", "", err
	}

	// Check for API token from environment (for CI/CD)
	if apiToken := os.Getenv("RACK_GATEWAY_API_TOKEN"); apiToken != "" {
		return normalized, apiToken, nil
	}

	tokenData, err := LoadToken(rack)
	if err != nil {
		return "", "", err
	}

	return normalized, tokenData.Token, nil
}

// GetCurrentRack returns the currently selected rack
func GetCurrentRack() (string, error) {
	cfg, exists, err := LoadConfig()
	if err != nil {
		return "", err
	}
	if !exists || strings.TrimSpace(cfg.Current) == "" {
		return "", fmt.Errorf("no current rack configured")
	}
	return strings.TrimSpace(cfg.Current), nil
}

// SetCurrentRack sets the currently selected rack
func SetCurrentRack(rack string) error {
	cfg, _, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.Current = rack
	return SaveConfig(cfg)
}

// UnsetCurrentRack removes the current rack selection
func UnsetCurrentRack() error {
	cfg, exists, err := LoadConfig()
	if err != nil {
		return err
	}
	if !exists || strings.TrimSpace(cfg.Current) == "" {
		return nil
	}
	cfg.Current = ""
	return SaveConfig(cfg)
}

// RemoveRack deletes the gateway config for a rack.
// Returns true if the rack existed, false if nothing changed.
func RemoveRack(rack string) (bool, error) {
	cfg, exists, err := LoadConfig()
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	changed := false
	if _, ok := cfg.Gateways[rack]; ok {
		delete(cfg.Gateways, rack)
		changed = true
	}
	if cfg.Current == rack {
		cfg.Current = ""
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := SaveConfig(cfg); err != nil {
		return false, err
	}
	return true, nil
}
