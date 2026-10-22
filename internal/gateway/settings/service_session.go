package settings

import "time"

// GetSessionTimeoutMinutes returns the session timeout in minutes.
func (s *Service) GetSessionTimeoutMinutes() (int, error) {
	return readGlobal(s, KeySessionTimeoutMinutes, nil, wholeNumber)
}

// GetSessionTimeoutDuration returns the session timeout as a time.Duration. On error it's the default timeout.
func (s *Service) GetSessionTimeoutDuration() (time.Duration, error) {
	minutes, err := s.GetSessionTimeoutMinutes()
	return time.Duration(minutes) * time.Minute, err
}
