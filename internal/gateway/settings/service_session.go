package settings

import "time"

// GetSessionTimeoutMinutes returns the session timeout in minutes.
func (s *Service) GetSessionTimeoutMinutes() (int, error) {
	setting, err := s.GetGlobalSetting(KeySessionTimeoutMinutes, 5)
	if err != nil {
		return 5, err
	}

	// Handle both int and float64 (JSON unmarshaling quirk)
	switch val := setting.Value.(type) {
	case int:
		return val, nil
	case float64:
		return int(val), nil
	default:
		return 5, nil
	}
}

// GetSessionTimeoutDuration returns the session timeout as a time.Duration.
func (s *Service) GetSessionTimeoutDuration() (time.Duration, error) {
	minutes, err := s.GetSessionTimeoutMinutes()
	if err != nil {
		return 5 * time.Minute, err
	}
	return time.Duration(minutes) * time.Minute, nil
}
