package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// UpsertRackTLSCert persists the pinned rack TLS certificate.
func (d *Database) UpsertRackTLSCert(cert *RackTLSCert, updatedByUserID *int64) error {
	if cert == nil {
		return fmt.Errorf("rack TLS certificate cannot be nil")
	}
	return d.UpsertSetting("rack_tls_cert", cert, updatedByUserID)
}
