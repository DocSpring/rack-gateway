package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Settings CRUD operations for unified settings table
// app_name NULL = global setting
// app_name non-null = app-specific setting

// GetSetting retrieves a setting value from the database.
// Returns the raw JSON bytes, a boolean indicating if found, and any error.
func (d *Database) GetSetting(appName *string, key string) ([]byte, bool, error) {
	var raw []byte
	var err error

	if appName == nil {
		err = d.queryRow(`SELECT value FROM settings WHERE app_name IS NULL AND key = ?`, key).Scan(&raw)
	} else {
		err = d.queryRow(`SELECT value FROM settings WHERE app_name = ? AND key = ?`, *appName, key).Scan(&raw)
	}

	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		scope := "global"
		if appName != nil {
			scope = fmt.Sprintf("app %s", *appName)
		}
		return nil, false, fmt.Errorf("failed to get %s setting %s: %w", scope, key, err)
	}
	return raw, true, nil
}

// GetAllSettings retrieves all settings for a given scope (global or app-specific).
// Returns a map of key -> raw JSON value.
func (d *Database) GetAllSettings(appName *string) (map[string][]byte, error) {
	var rows *sql.Rows
	var err error

	if appName == nil {
		rows, err = d.query(`SELECT key, value FROM settings WHERE app_name IS NULL`)
	} else {
		rows, err = d.query(`SELECT key, value FROM settings WHERE app_name = ?`, *appName)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get all settings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	settings := make(map[string][]byte)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}
		settings[key] = value
	}

	return settings, rows.Err()
}

// SQL constants for settings upsert operations
const (
	upsertSettingWithUserGlobalSQL = `
		INSERT INTO settings (app_name, key, value, updated_at, updated_by_user_id)
		VALUES (NULL, ?, ?::jsonb, NOW(), ?)
		ON CONFLICT (COALESCE(app_name, ''), key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = NOW(), updated_by_user_id = EXCLUDED.updated_by_user_id`
	upsertSettingWithUserAppSQL = `
		INSERT INTO settings (app_name, key, value, updated_at, updated_by_user_id)
		VALUES (?, ?, ?::jsonb, NOW(), ?)
		ON CONFLICT (COALESCE(app_name, ''), key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = NOW(), updated_by_user_id = EXCLUDED.updated_by_user_id`
	upsertSettingNoUserGlobalSQL = `
		INSERT INTO settings (app_name, key, value, updated_at)
		VALUES (NULL, ?, ?::jsonb, NOW())
		ON CONFLICT (COALESCE(app_name, ''), key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = NOW()`
	upsertSettingNoUserAppSQL = `
		INSERT INTO settings (app_name, key, value, updated_at)
		VALUES (?, ?, ?::jsonb, NOW())
		ON CONFLICT (COALESCE(app_name, ''), key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = NOW()`
)

// upsertSettingCore is the centralized helper for all setting upsert operations.
// If tx is nil, it uses the database connection directly; otherwise it uses the transaction.
func (d *Database) upsertSettingCore(tx *sql.Tx, appName *string, key, valueJSON string, userID *int64) error {
	execFn := func(query string, args ...interface{}) (sql.Result, error) {
		if tx != nil {
			return d.execTx(tx, query, args...)
		}
		return d.exec(query, args...)
	}

	if userID != nil {
		if appName == nil {
			_, err := execFn(upsertSettingWithUserGlobalSQL, key, valueJSON, *userID)
			return err
		}
		_, err := execFn(upsertSettingWithUserAppSQL, *appName, key, valueJSON, *userID)
		return err
	}

	if appName == nil {
		_, err := execFn(upsertSettingNoUserGlobalSQL, key, valueJSON)
		return err
	}
	_, err := execFn(upsertSettingNoUserAppSQL, *appName, key, valueJSON)
	return err
}

// UpsertSetting creates or updates a setting value.
func (d *Database) UpsertSetting(appName *string, key string, value interface{}, updatedByUserID *int64) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal setting %s: %w", key, err)
	}

	if err := d.upsertSettingCore(nil, appName, key, string(b), updatedByUserID); err != nil {
		scope := "global"
		if appName != nil {
			scope = fmt.Sprintf("app %s", *appName)
		}
		return fmt.Errorf("failed to upsert %s setting %s: %w", scope, key, err)
	}
	return nil
}

// SettingUpdate represents a single setting to update in a batch operation.
type SettingUpdate struct {
	Key   string
	Value interface{}
}

// UpsertSettingsInTx atomically updates multiple settings within a single transaction.
// All settings succeed or all fail together.
func (d *Database) UpsertSettingsInTx(appName *string, updates []SettingUpdate, updatedByUserID *int64) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin settings transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, update := range updates {
		valueJSON, err := json.Marshal(update.Value)
		if err != nil {
			return fmt.Errorf("failed to marshal setting %s: %w", update.Key, err)
		}

		if err := d.upsertSettingCore(tx, appName, update.Key, string(valueJSON), updatedByUserID); err != nil {
			scope := "global"
			if appName != nil {
				scope = fmt.Sprintf("app %s", *appName)
			}
			return fmt.Errorf("failed to upsert %s setting %s: %w", scope, update.Key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit settings transaction: %w", err)
	}
	return nil
}

// DeleteSetting removes a setting from the database (revert to env/default).
func (d *Database) DeleteSetting(appName *string, key string) error {
	var err error
	if appName == nil {
		_, err = d.exec(`DELETE FROM settings WHERE app_name IS NULL AND key = ?`, key)
	} else {
		_, err = d.exec(`DELETE FROM settings WHERE app_name = ? AND key = ?`, *appName, key)
	}

	if err != nil {
		scope := "global"
		if appName != nil {
			scope = fmt.Sprintf("app %s", *appName)
		}
		return fmt.Errorf("failed to delete %s setting %s: %w", scope, key, err)
	}
	return nil
}

// MFASettings capture system-wide MFA configuration values.
type MFASettings struct {
	RequireAllUsers      bool `json:"require_all_users"`
	TrustedDeviceTTLDays int  `json:"trusted_device_ttl_days"`
	StepUpWindowMinutes  int  `json:"step_up_window_minutes"`
}

// GetRackTLSCert returns the pinned rack TLS certificate if it exists.
func (d *Database) GetRackTLSCert() (*RackTLSCert, bool, error) {
	raw, ok, err := d.GetSetting(nil, "rack_tls_cert")
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
	return d.UpsertSetting(nil, "rack_tls_cert", cert, updatedByUserID)
}
