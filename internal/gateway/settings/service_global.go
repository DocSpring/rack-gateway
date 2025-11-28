package settings

import "github.com/DocSpring/rack-gateway/internal/gateway/db"

// GetGlobalSetting retrieves a global setting.
func (s *Service) GetGlobalSetting(key string, defaultValue interface{}) (*Setting, error) {
	return s.getSetting(nil, key, defaultValue)
}

// GetAllGlobalSettings retrieves all global settings with environment fallback.
func (s *Service) GetAllGlobalSettings() (map[string]*Setting, error) {
	rawSettings, err := s.db.GetAllSettings(nil)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*Setting, len(DefaultGlobalSettings))
	for key, defaultValue := range DefaultGlobalSettings {
		setting, err := s.resolveSetting(nil, key, defaultValue, rawSettings)
		if err != nil {
			return nil, err
		}
		result[key] = setting
	}

	return result, nil
}

// SetGlobalSetting saves a global setting to the database.
func (s *Service) SetGlobalSetting(key string, value interface{}, updatedByUserID *int64) error {
	return s.db.UpsertSetting(nil, key, value, updatedByUserID)
}

// DeleteGlobalSetting removes a global setting from the database (reverts to env/default).
func (s *Service) DeleteGlobalSetting(key string) error {
	return s.db.DeleteSetting(nil, key)
}

// SetGlobalSettingsInTx atomically updates multiple global settings in a single transaction.
func (s *Service) SetGlobalSettingsInTx(updates []db.SettingUpdate, updatedByUserID *int64) error {
	return s.db.UpsertSettingsInTx(nil, updates, updatedByUserID)
}
