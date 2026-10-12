package settings

import (
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// GetMFASettings returns the MFA configuration as it is now, read with one query. It's called for each request
// that needs it, so a change applies at once, on every gateway instance.
func (s *Service) GetMFASettings() (*db.MFASettings, error) {
	stored, err := s.db.GetAllSettings(nil)
	if err != nil {
		return nil, err
	}
	resolve := func(key string) (interface{}, error) {
		setting, err := s.resolveSetting(nil, key, DefaultGlobalSettings[key], stored)
		if err != nil {
			return nil, err
		}
		return setting.Value, nil
	}

	requireAll, err := resolve(KeyMFARequireAllUsers)
	if err != nil {
		return nil, err
	}
	ttlDays, err := resolve(KeyTrustedDeviceTTLDays)
	if err != nil {
		return nil, err
	}
	stepUpMinutes, err := resolve(KeyStepUpWindowMinutes)
	if err != nil {
		return nil, err
	}

	requireAllBool, ok := requireAll.(bool)
	if !ok {
		requireAllBool = true
	}
	return &db.MFASettings{
		RequireAllUsers:      requireAllBool,
		TrustedDeviceTTLDays: intValue(ttlDays),
		StepUpWindowMinutes:  intValue(stepUpMinutes),
	}, nil
}

// intValue is a numeric setting's value as an int: JSON numbers decode as float64. Anything else is 0, which
// db.MFASettings treats as unset.
func intValue(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

// SetMFASettings stores MFA configuration in the database atomically.
// All settings are updated within a single transaction to prevent partial updates.
func (s *Service) SetMFASettings(settings *db.MFASettings, updatedByUserID *int64) error {
	if settings == nil {
		return fmt.Errorf("mfa settings cannot be nil")
	}
	if settings.TrustedDeviceTTLDays <= 0 {
		settings.TrustedDeviceTTLDays = defaultTrustedDeviceTTLDays
	}
	if settings.StepUpWindowMinutes <= 0 {
		settings.StepUpWindowMinutes = defaultStepUpWindowMinutes
	}

	updates := []db.SettingUpdate{
		{Key: KeyMFARequireAllUsers, Value: settings.RequireAllUsers},
		{Key: KeyTrustedDeviceTTLDays, Value: settings.TrustedDeviceTTLDays},
		{Key: KeyStepUpWindowMinutes, Value: settings.StepUpWindowMinutes},
	}

	return s.SetGlobalSettingsInTx(updates, updatedByUserID)
}
