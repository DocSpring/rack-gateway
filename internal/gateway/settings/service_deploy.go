package settings

// GetAllowDestructiveActions returns whether destructive actions are allowed.
func (s *Service) GetAllowDestructiveActions() (bool, error) {
	return readGlobal(s, KeyAllowDestructiveActions, nil, boolValue)
}

// GetDeployApprovalsEnabled returns whether deploy approvals are enabled.
func (s *Service) GetDeployApprovalsEnabled() (bool, error) {
	return readGlobal(s, KeyDeployApprovalsEnabled, nil, boolValue)
}

// GetDeployApprovalRequireDifferentApprover returns whether a deploy approval must be approved (and extended) by
// someone other than the user who requested it or owns its API token.
func (s *Service) GetDeployApprovalRequireDifferentApprover() (bool, error) {
	return readGlobal(s, KeyDeployApprovalRequireDifferentApprover, nil, boolValue)
}

// GetDeployApprovalWindowMinutes returns the deploy approval window in minutes.
func (s *Service) GetDeployApprovalWindowMinutes() (int, error) {
	return readGlobal(s, KeyDeployApprovalWindowMinutes, nil, wholeNumber)
}
