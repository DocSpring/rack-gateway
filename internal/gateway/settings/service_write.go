package settings

import (
	"maps"
	"slices"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// SetGlobalSettings saves global settings together: all of them or none (see setSettings).
func (s *Service) SetGlobalSettings(values map[string]interface{}, updatedByUserID *int64) error {
	return s.setSettings(nil, values, updatedByUserID)
}

// SetAppSettings saves an app's settings together: all of them or none (see setSettings).
func (s *Service) SetAppSettings(appName string, values map[string]interface{}, updatedByUserID *int64) error {
	return s.setSettings(&appName, values, updatedByUserID)
}

// setSettings checks every value, then saves them in one transaction. A value that doesn't fit its setting is
// refused (ErrInvalidSettingValue), and so is a change to a setting its environment variable defines
// (ErrSetByEnvironment). Submitting the value such a setting already has is skipped, so forms that submit every
// field still work. appName is nil for global settings.
func (s *Service) setSettings(appName *string, values map[string]interface{}, updatedByUserID *int64) error {
	updates := make([]db.SettingUpdate, 0, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value := values[key]
		if err := ValidateSettingValue(appName, key, value); err != nil {
			return err
		}
		store, err := checkEnvDefined(appName, key, value)
		if err != nil {
			return err
		}
		if store {
			updates = append(updates, db.SettingUpdate{Key: key, Value: value})
		}
	}
	return s.db.UpsertSettingsInTx(appName, updates, updatedByUserID)
}

// DeleteGlobalSettings removes global settings' database values together, so their environment variables or
// defaults apply.
func (s *Service) DeleteGlobalSettings(keys []string) error {
	return s.db.DeleteSettings(nil, keys)
}

// DeleteAppSettings removes an app's settings' database values together, so their environment variables or
// defaults apply.
func (s *Service) DeleteAppSettings(appName string, keys []string) error {
	return s.db.DeleteSettings(&appName, keys)
}
