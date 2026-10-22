package settings

// GetGlobalSetting retrieves a global setting.
func (s *Service) GetGlobalSetting(key string, defaultValue interface{}) (*Setting, error) {
	return s.getSetting(nil, key, defaultValue)
}

// GetAllGlobalSettings resolves every global setting.
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
