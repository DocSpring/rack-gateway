package settings

import "github.com/DocSpring/rack-gateway/internal/gateway/db"

// GetMFASettings returns the MFA configuration as it is now, read with one query. It's called for each request
// that needs it, so a change applies at once, on every gateway instance. A stored value that doesn't fit its
// setting is an error (see readGlobal): the gateway refuses to start, and requests that need MFA fail closed.
func (s *Service) GetMFASettings() (*db.MFASettings, error) {
	stored, err := s.db.GetAllSettings(nil)
	if err != nil {
		return nil, err
	}
	requireAll, err := readGlobal(s, KeyMFARequireAllUsers, stored, boolValue)
	if err != nil {
		return nil, err
	}
	ttlDays, err := readGlobal(s, KeyTrustedDeviceTTLDays, stored, wholeNumber)
	if err != nil {
		return nil, err
	}
	stepUpMinutes, err := readGlobal(s, GlobalSettingStepUpWindowMinutes.String(), stored, wholeNumber)
	if err != nil {
		return nil, err
	}
	return &db.MFASettings{
		RequireAllUsers:      requireAll,
		TrustedDeviceTTLDays: ttlDays,
		StepUpWindowMinutes:  stepUpMinutes,
	}, nil
}
