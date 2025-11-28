package settings

// GetAllowDestructiveActions returns whether destructive actions are allowed.
func (s *Service) GetAllowDestructiveActions() (bool, error) {
	setting, err := s.GetGlobalSetting(KeyAllowDestructiveActions, false)
	if err != nil {
		return false, err
	}

	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}

	return false, nil
}

// GetDeployApprovalsEnabled returns whether deploy approvals are enabled.
func (s *Service) GetDeployApprovalsEnabled() (bool, error) {
	setting, err := s.GetGlobalSetting(KeyDeployApprovalsEnabled, true)
	if err != nil {
		return true, err
	}

	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}

	return true, nil
}

// GetDeployApprovalWindowMinutes returns the deploy approval window in minutes.
func (s *Service) GetDeployApprovalWindowMinutes() (int, error) {
	setting, err := s.GetGlobalSetting(KeyDeployApprovalWindowMinutes, 15)
	if err != nil {
		return 15, err
	}

	// Handle both int and float64 (JSON unmarshaling quirk)
	switch val := setting.Value.(type) {
	case int:
		return val, nil
	case float64:
		return int(val), nil
	default:
		return 15, nil
	}
}
