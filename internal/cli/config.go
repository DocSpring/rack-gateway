package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func configFile() string {
	return filepath.Join(ConfigPath, "config.json")
}

// LoadConfig loads the CLI configuration from disk. The bool reports whether the file existed. A config without a
// machine ID or MFA preference gets the defaults, saved under the config lock.
func LoadConfig() (*Config, bool, error) {
	cfg, exists, err := readConfig()
	if err != nil || !applyConfigDefaults(cfg) {
		return cfg, exists, err
	}
	err = updateConfig(func(latest *Config) (bool, error) {
		cfg = latest
		return false, nil
	})
	if err != nil {
		return nil, exists, err
	}
	return cfg, exists, nil
}

// readConfig reads config.json, or an empty config when there is none.
func readConfig() (*Config, bool, error) {
	cfg := &Config{}
	data, err := fs.ReadFile(os.DirFS(ConfigPath), "config.json")
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	if exists {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, false, err
		}
	}
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	return cfg, exists, nil
}

// applyConfigDefaults fills in a missing machine ID and MFA preference, and reports whether it changed cfg.
func applyConfigDefaults(cfg *Config) bool {
	changed := false
	if strings.TrimSpace(cfg.MachineID) == "" {
		cfg.MachineID = uuid.NewString()
		changed = true
	}
	if cfg.MFAPreference == "" {
		cfg.MFAPreference = "default" // Default to user's profile preference
		changed = true
	}
	return changed
}

// SaveConfig writes the CLI configuration to disk. Commands change the config through updateConfig, which holds the
// config lock. The file holds session tokens, so it is only ever readable by its owner: the directory is kept at
// 0700 and the file at 0600 (tightened if an earlier version or the user made them looser), and the file is
// replaced atomically, so a crash never leaves it truncated.
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

// SaveLogin saves a completed login: the rack's gateway URL and session, in one change that also makes it the
// current rack. A rack that already uses this gateway keeps its settings. Nothing is saved before a login
// succeeds, so a mistyped URL or a canceled login leaves the rack's existing session as it was.
func SaveLogin(rack, gatewayURL string, loginResp *LoginResponse) error {
	return updateConfig(func(cfg *Config) (bool, error) {
		gateway, ok := cfg.Gateways[rack]
		if configured, err := NormalizeGatewayURL(gateway.URL); !ok || err != nil || configured != gatewayURL {
			gateway = GatewayConfig{URL: gatewayURL}
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
		cfg.Current = rack
		return true, nil
	})
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

// apiTokenOverride is the API token the command runs with: --api-token, else RACK_GATEWAY_API_TOKEN. Empty means
// the rack's stored session.
func apiTokenOverride() string {
	if token := strings.TrimSpace(APITokenFlag); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("RACK_GATEWAY_API_TOKEN"))
}

// LoadRackAuth loads the gateway URL and token for a rack. With an API token (--api-token or
// RACK_GATEWAY_API_TOKEN, for CI/CD), RACK_GATEWAY_URL replaces the rack's configured URL. A stored session token
// is only ever sent to the gateway that issued it, so without an API token RACK_GATEWAY_URL must match the rack's
// configured URL.
func LoadRackAuth(rack string) (string, string, error) {
	envURL := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL"))
	apiToken := apiTokenOverride()
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
				"unset RACK_GATEWAY_URL, or pass --api-token (or RACK_GATEWAY_API_TOKEN) to use it with an API token",
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
	return updateConfig(func(cfg *Config) (bool, error) {
		cfg.Current = rack
		return true, nil
	})
}

// RemoveRack deletes the gateway config for a rack.
// Returns true if the rack existed, false if nothing changed.
func RemoveRack(rack string) (bool, error) {
	removed := false
	err := updateConfig(func(cfg *Config) (bool, error) {
		if _, ok := cfg.Gateways[rack]; ok {
			delete(cfg.Gateways, rack)
			removed = true
		}
		if cfg.Current == rack {
			cfg.Current = ""
			removed = true
		}
		return removed, nil
	})
	return removed, err
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
