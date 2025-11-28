package settings

// GetAppSetting retrieves an app-specific setting.
// For VCS/CI provider fields, if the app setting is nil/empty, it will use the global default.
func (s *Service) GetAppSetting(appName, key string, defaultValue interface{}) (*Setting, error) {
	setting, err := s.getSetting(&appName, key, defaultValue)
	if err != nil {
		return nil, err
	}

	// Map of app setting keys to their corresponding global default keys
	globalDefaultKeys := map[string]string{KeyVCSProvider: KeyDefaultVCSProvider, KeyCIProvider: KeyDefaultCIProvider}

	// Check if this setting should fall back to a global default
	if globalKey, hasGlobal := globalDefaultKeys[key]; hasGlobal && (setting.Value == nil || setting.Value == "") {
		globalDefault := DefaultGlobalSettings[globalKey]
		globalSetting, err := s.GetGlobalSetting(globalKey, globalDefault)
		if err != nil {
			return nil, err
		}
		setting.Value = globalSetting.Value
		setting.Source = SourceGlobalDefault
	}

	return setting, nil
}

// GetAllAppSettings retrieves all app-specific settings with environment fallback.
// For VCS/CI provider fields, if the app setting is nil/empty, it will use the global default
// and mark the source as SourceGlobalDefault.
func (s *Service) GetAllAppSettings(appName string) (map[string]*Setting, error) {
	rawAppSettings, err := s.db.GetAllSettings(&appName)
	if err != nil {
		return nil, err
	}

	globalSettings, err := s.GetAllGlobalSettings()
	if err != nil {
		return nil, err
	}

	// Map of app setting keys to their corresponding global default keys
	globalDefaultKeys := map[string]string{
		KeyVCSProvider: KeyDefaultVCSProvider,
		KeyCIProvider:  KeyDefaultCIProvider,
	}

	result := make(map[string]*Setting, len(DefaultAppSettings))
	for key, defaultValue := range DefaultAppSettings {
		setting, err := s.resolveSetting(&appName, key, defaultValue, rawAppSettings)
		if err != nil {
			return nil, err
		}

		// Check if this setting should fall back to a global default
		if globalKey, hasGlobal := globalDefaultKeys[key]; hasGlobal {
			// If app setting is nil or empty string, use global default
			if setting.Value == nil || setting.Value == "" {
				if globalSetting, ok := globalSettings[globalKey]; ok {
					setting.Value = globalSetting.Value
					setting.Source = SourceGlobalDefault
				}
			}
		}

		result[key] = setting
	}

	return result, nil
}

// SetAppSetting saves an app-specific setting to the database.
func (s *Service) SetAppSetting(appName, key string, value interface{}, updatedByUserID *int64) error {
	return s.db.UpsertSetting(&appName, key, value, updatedByUserID)
}

// DeleteAppSetting removes an app-specific setting from the database (reverts to env/default).
func (s *Service) DeleteAppSetting(appName, key string) error {
	return s.db.DeleteSetting(&appName, key)
}
