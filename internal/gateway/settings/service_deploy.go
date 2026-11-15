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

// DeployApprovalAutoApproval is what decides whether the gateway approves a deploy approval request as it's created.
type DeployApprovalAutoApproval struct {
	// Enabled is deploy_approval_auto_approve, for a rack where CI's own approval step is the approval
	Enabled bool
	// RequireDifferentApprover is deploy_approval_require_different_approver, which auto-approval never overrides
	RequireDifferentApprover bool
}

// GetDeployApprovalAutoApproval reads both settings with one query, so a request created while an admin changes
// them together sees the old values or the new ones, never one of each.
func (s *Service) GetDeployApprovalAutoApproval() (DeployApprovalAutoApproval, error) {
	stored, err := s.db.GetAllSettings(nil)
	if err != nil {
		return DeployApprovalAutoApproval{}, err
	}
	enabled, err := readGlobal(s, KeyDeployApprovalAutoApprove, stored, boolValue)
	if err != nil {
		return DeployApprovalAutoApproval{}, err
	}
	differentApprover, err := readGlobal(s, KeyDeployApprovalRequireDifferentApprover, stored, boolValue)
	if err != nil {
		return DeployApprovalAutoApproval{}, err
	}
	return DeployApprovalAutoApproval{Enabled: enabled, RequireDifferentApprover: differentApprover}, nil
}

// GetDeployApprovalWindowMinutes returns the deploy approval window in minutes.
func (s *Service) GetDeployApprovalWindowMinutes() (int, error) {
	return readGlobal(s, KeyDeployApprovalWindowMinutes, nil, wholeNumber)
}
