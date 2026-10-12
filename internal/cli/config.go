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
	//nolint:gosec // G304: Path from configFile() is application-controlled
	data, err := os.ReadFile(path)
	exists := true
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		exists = false
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

// SaveConfig saves the CLI configuration to disk. The file holds session tokens, so it is only ever readable by
// its owner: the directory is kept at 0700 and the file at 0600 (tightened if an earlier version or the user made
// them looser), and the file is replaced atomically, so a crash or a concurrent command never leaves it truncated.
func SaveConfig(cfg *Config) error {
	if err := ensurePrivateDir(ConfigPath); err != nil {
		return err
	}
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFileAtomically(configFile(), data); err != nil {
		return err
	}
	// Remove standalone current file if it exists so config.json remains the source of truth.
	_ = os.Remove(filepath.Join(ConfigPath, "current"))
	return nil
}

// ensurePrivateDir creates dir with mode 0700, or takes the group and other permissions away from an existing one.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return os.Chmod(dir, perm&^0o077)
	}
	return nil
}

// writePrivateFileAtomically replaces path with data. It writes a temporary file in the same directory (which
// os.CreateTemp creates with mode 0600), syncs it to disk and renames it over path: readers see the old file or
// the new one, never part of one, and the new file replaces any looser permissions the old one had.
func writePrivateFileAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// writeAndClose writes data to file, syncs it to disk and closes it.
func writeAndClose(file *os.File, data []byte) error {
	_, err := file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
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
		return nil, ErrTokenExpired
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

// LoadRackAuth loads the gateway URL and token for a rack. With an API token (RACK_GATEWAY_API_TOKEN, for CI/CD),
// RACK_GATEWAY_URL replaces the rack's configured URL. A stored session token is only ever sent to the gateway that
// issued it, so without an API token RACK_GATEWAY_URL must match the rack's configured URL.
func LoadRackAuth(rack string) (string, string, error) {
	envURL := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL"))
	apiToken := os.Getenv("RACK_GATEWAY_API_TOKEN")
	if envURL != "" && apiToken != "" {
		normalized, err := NormalizeGatewayURL(envURL)
		if err != nil {
			return "", "", err
		}
		return normalized, apiToken, nil
	}

	configuredURL, err := LoadGatewayURL(rack)
	if err != nil {
		return "", "", err
	}
	normalized, err := NormalizeGatewayURL(configuredURL)
	if err != nil {
		return "", "", err
	}
	if err := checkGatewayURLOverride(envURL, rack, normalized); err != nil {
		return "", "", err
	}

	if apiToken != "" {
		return normalized, apiToken, nil
	}

	tokenData, err := LoadToken(rack)
	if err != nil {
		return "", "", err
	}

	return normalized, tokenData.Token, nil
}

// checkGatewayURLOverride refuses a RACK_GATEWAY_URL (envURL, empty when unset) that isn't the rack's configured
// gateway, so the rack's session token is never sent to another host.
func checkGatewayURLOverride(envURL, rack, configuredURL string) error {
	if envURL == "" {
		return nil
	}
	override, err := NormalizeGatewayURL(envURL)
	if err != nil {
		return err
	}
	if override != configuredURL {
		return fmt.Errorf(
			"RACK_GATEWAY_URL (%s) is not the gateway of rack %s (%s), so its session token can't be sent there; "+
				"unset RACK_GATEWAY_URL, or set RACK_GATEWAY_API_TOKEN to use it with an API token",
			override, rack, configuredURL,
		)
	}
	return nil
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

// touchNotificationsEnabled reports whether to show a desktop notification each time a security key touch is
// needed (the touch_notifications setting, on by default).
func touchNotificationsEnabled() bool {
	cfg, _, err := LoadConfig()
	if err != nil || cfg.TouchNotifications == nil {
		return true
	}
	return *cfg.TouchNotifications
}
