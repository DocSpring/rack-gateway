package settings

import (
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// GetMFASettings returns MFA configuration with environment fallback.
func (s *Service) GetMFASettings() (*db.MFASettings, error) {
	requireAll, err := s.GetGlobalSetting(KeyMFARequireAllUsers, true)
	if err != nil {
		return nil, err
	}

	ttlDays, err := s.GetGlobalSetting(KeyTrustedDeviceTTLDays, 30)
	if err != nil {
		return nil, err
	}

	stepUpMinutes, err := s.GetGlobalSetting(KeyStepUpWindowMinutes, 10)
	if err != nil {
		return nil, err
	}

	// Convert values to correct types
	requireAllBool, ok := requireAll.Value.(bool)
	if !ok {
		requireAllBool = true
	}

	ttlDaysInt, ok := ttlDays.Value.(int)
	if !ok {
		if f, ok := ttlDays.Value.(float64); ok {
			ttlDaysInt = int(f)
		} else {
			ttlDaysInt = 30
		}
	}

	stepUpMinutesInt, ok := stepUpMinutes.Value.(int)
	if !ok {
		if f, ok := stepUpMinutes.Value.(float64); ok {
			stepUpMinutesInt = int(f)
		} else {
			stepUpMinutesInt = 10
		}
	}

	return &db.MFASettings{
		RequireAllUsers:      requireAllBool,
		TrustedDeviceTTLDays: ttlDaysInt,
		StepUpWindowMinutes:  stepUpMinutesInt,
	}, nil
}

// SetMFASettings stores MFA configuration in the database.
func (s *Service) SetMFASettings(settings *db.MFASettings, updatedByUserID *int64) error {
	if settings == nil {
		return fmt.Errorf("mfa settings cannot be nil")
	}
	if settings.TrustedDeviceTTLDays <= 0 {
		settings.TrustedDeviceTTLDays = 30
	}
	if settings.StepUpWindowMinutes <= 0 {
		settings.StepUpWindowMinutes = 10
	}

	if err := s.SetGlobalSetting(KeyMFARequireAllUsers, settings.RequireAllUsers, updatedByUserID); err != nil {
		return err
	}
	if err := s.SetGlobalSetting(KeyTrustedDeviceTTLDays, settings.TrustedDeviceTTLDays, updatedByUserID); err != nil {
		return err
	}
	return s.SetGlobalSetting(KeyStepUpWindowMinutes, settings.StepUpWindowMinutes, updatedByUserID)
}
