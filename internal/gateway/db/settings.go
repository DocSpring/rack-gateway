package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Settings helpers

// GetSettingRaw returns the raw JSON value for a setting key.
func (d *Database) GetSettingRaw(key string) ([]byte, bool, error) {
	var raw []byte
	err := d.queryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to get setting %s: %w", key, err)
	}
	return raw, true, nil
}

// UpsertSetting sets the setting value (as JSON) with optional updated_by_user_id.
func (d *Database) UpsertSetting(key string, value interface{}, updatedByUserID *int64) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal setting %s: %w", key, err)
	}
	if updatedByUserID != nil {
		_, err = d.exec(`INSERT INTO settings (key, value, updated_at, updated_by_user_id)
            VALUES (?, ?::jsonb, NOW(), ?)
            ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW(), updated_by_user_id = EXCLUDED.updated_by_user_id`, key, string(b), *updatedByUserID)
	} else {
		_, err = d.exec(`INSERT INTO settings (key, value, updated_at)
            VALUES (?, ?::jsonb, NOW())
            ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`, key, string(b))
	}
	if err != nil {
		return fmt.Errorf("failed to upsert setting %s: %w", key, err)
	}
	return nil
}

// GetProtectedEnvVars returns the list of protected env var names (normalized upper-case unique).
func (d *Database) GetProtectedEnvVars() ([]string, error) {
	raw, ok, err := d.GetSettingRaw("protected_env_vars")
	if err != nil || !ok {
		return []string{}, err
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return []string{}, fmt.Errorf("invalid protected_env_vars setting: %w", err)
	}
	// normalize
	seen := map[string]struct{}{}
	out := make([]string, 0, len(arr))
	for _, k := range arr {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out, nil
}

// GetAllowDestructiveActions returns whether destructive actions are allowed (default false).
func (d *Database) GetAllowDestructiveActions() (bool, error) {
	raw, ok, err := d.GetSettingRaw("allow_destructive_actions")
	if err != nil || !ok {
		return false, err
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("invalid allow_destructive_actions setting: %w", err)
	}
	return v, nil
}

// GetRackTLSCert returns the pinned rack TLS certificate if it exists.
func (d *Database) GetRackTLSCert() (*RackTLSCert, bool, error) {
	raw, ok, err := d.GetSettingRaw("rack_tls_cert")
	if err != nil || !ok {
		return nil, ok, err
	}
	var cert RackTLSCert
	if err := json.Unmarshal(raw, &cert); err != nil {
		return nil, false, fmt.Errorf("invalid rack_tls_cert setting: %w", err)
	}
	return &cert, true, nil
}

// MFASettings capture system-wide MFA configuration values.
type MFASettings struct {
	RequireAllUsers      bool `json:"require_all_users"`
	TrustedDeviceTTLDays int  `json:"trusted_device_ttl_days"`
	StepUpWindowMinutes  int  `json:"step_up_window_minutes"`
}

func defaultRequireAllUsers() bool {
	if v := strings.TrimSpace(os.Getenv("MFA_REQUIRE_ALL_USERS")); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			return parsed
		}
	}
	return true
}

// UpsertRackTLSCert persists the pinned rack TLS certificate.
func (d *Database) UpsertRackTLSCert(cert *RackTLSCert, updatedByUserID *int64) error {
	if cert == nil {
		return fmt.Errorf("rack TLS certificate cannot be nil")
	}
	return d.UpsertSetting("rack_tls_cert", cert, updatedByUserID)
}

// GetMFASettings returns MFA configuration with sensible defaults when not set.
func (d *Database) GetMFASettings() (*MFASettings, error) {
	defaults := &MFASettings{
		RequireAllUsers:      defaultRequireAllUsers(),
		TrustedDeviceTTLDays: 30,
		StepUpWindowMinutes:  10,
	}

	raw, ok, err := d.GetSettingRaw("mfa")
	if err != nil {
		return defaults, err
	}
	if !ok || len(raw) == 0 {
		return defaults, nil
	}

	var settings MFASettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return defaults, fmt.Errorf("invalid mfa settings: %w", err)
	}

	if settings.TrustedDeviceTTLDays <= 0 {
		settings.TrustedDeviceTTLDays = defaults.TrustedDeviceTTLDays
	}
	if settings.StepUpWindowMinutes <= 0 {
		settings.StepUpWindowMinutes = defaults.StepUpWindowMinutes
	}

	return &settings, nil
}

// UpsertMFASettings stores the MFA configuration settings atomically.
func (d *Database) UpsertMFASettings(settings *MFASettings, updatedByUserID *int64) error {
	if settings == nil {
		return fmt.Errorf("mfa settings cannot be nil")
	}
	if settings.TrustedDeviceTTLDays <= 0 {
		settings.TrustedDeviceTTLDays = 30
	}
	if settings.StepUpWindowMinutes <= 0 {
		settings.StepUpWindowMinutes = 10
	}
	return d.UpsertSetting("mfa", settings, updatedByUserID)
}

// ApprovedCommandsSettings contains the list of commands allowed for CI/CD exec.
type ApprovedCommandsSettings struct {
	Commands []string `json:"commands"`
}

// GetApprovedCommands returns the list of approved commands for CI/CD exec.
func (d *Database) GetApprovedCommands() ([]string, error) {
	raw, ok, err := d.GetSettingRaw("approved_commands")
	if err != nil {
		return []string{}, err
	}
	if !ok || len(raw) == 0 {
		return []string{}, nil
	}

	var settings ApprovedCommandsSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return []string{}, fmt.Errorf("invalid approved_commands setting: %w", err)
	}

	return settings.Commands, nil
}

// UpdateApprovedCommands sets the list of approved commands.
func (d *Database) UpdateApprovedCommands(commands []string, updatedByUserID *int64) error {
	settings := &ApprovedCommandsSettings{Commands: commands}
	return d.UpsertSetting("approved_commands", settings, updatedByUserID)
}

// CircleCISettings contains CircleCI integration configuration.
type CircleCISettings struct {
	APIToken        string `json:"api_token"`
	ApprovalJobName string `json:"approval_job_name"`
	OrgSlug         string `json:"org_slug,omitempty"`
}

// GetCircleCISettings returns CircleCI integration settings.
func (d *Database) GetCircleCISettings() (*CircleCISettings, error) {
	raw, ok, err := d.GetSettingRaw("circleci")
	if err != nil {
		return nil, err
	}
	if !ok || len(raw) == 0 {
		return &CircleCISettings{}, nil
	}

	var settings CircleCISettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("invalid circleci settings: %w", err)
	}

	return &settings, nil
}

// UpsertCircleCISettings stores CircleCI integration settings.
func (d *Database) UpsertCircleCISettings(settings *CircleCISettings, updatedByUserID *int64) error {
	if settings == nil {
		return fmt.Errorf("circleci settings cannot be nil")
	}
	return d.UpsertSetting("circleci", settings, updatedByUserID)
}

// CircleCIEnabled returns true if CircleCI integration is configured.
func (d *Database) CircleCIEnabled() (bool, error) {
	settings, err := d.GetCircleCISettings()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(settings.APIToken) != "" && strings.TrimSpace(settings.ApprovalJobName) != "", nil
}
