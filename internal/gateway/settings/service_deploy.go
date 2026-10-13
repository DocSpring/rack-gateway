package settings

// globalBool returns a boolean global setting, or defaultValue when it isn't a boolean. On error it returns
// defaultValue with the error, so the caller decides whether to fail closed.
func (s *Service) globalBool(key string, defaultValue bool) (bool, error) {
	setting, err := s.GetGlobalSetting(key, defaultValue)
	if err != nil {
		return defaultValue, err
	}
	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}
	return defaultValue, nil
}

// GetAllowDestructiveActions returns whether destructive actions are allowed.
func (s *Service) GetAllowDestructiveActions() (bool, error) {
	return s.globalBool(KeyAllowDestructiveActions, false)
}

// GetDeployApprovalsEnabled returns whether deploy approvals are enabled.
func (s *Service) GetDeployApprovalsEnabled() (bool, error) {
	return s.globalBool(KeyDeployApprovalsEnabled, true)
}

// GetDeployApprovalRequireDifferentApprover returns whether a deploy approval must be approved (and extended) by
// someone other than the user who requested it or owns its API token.
func (s *Service) GetDeployApprovalRequireDifferentApprover() (bool, error) {
	return s.globalBool(KeyDeployApprovalRequireDifferentApprover, false)
}

// GetDeployApprovalWindowMinutes returns the deploy approval window in minutes.
func (s *Service) GetDeployApprovalWindowMinutes() (int, error) {
	setting, err := s.GetGlobalSetting(KeyDeployApprovalWindowMinutes, DefaultDeployApprovalWindowMinutes)
	if err != nil {
		return DefaultDeployApprovalWindowMinutes, err
	}

	// Handle both int and float64 (JSON unmarshaling quirk)
	switch val := setting.Value.(type) {
	case int:
		return val, nil
	case float64:
		return int(val), nil
	default:
		return DefaultDeployApprovalWindowMinutes, nil
	}
}
