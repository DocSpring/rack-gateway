package db

import (
	"errors"
)

type rowScanner interface {
	Scan(dest ...interface{}) error
}

// Deploy approval request status constants
const (
	DeployApprovalRequestStatusPending  = "pending"
	DeployApprovalRequestStatusApproved = "approved"
	DeployApprovalRequestStatusRejected = "rejected"
	DeployApprovalRequestStatusExpired  = "expired"
	DeployApprovalRequestStatusDeployed = "deployed"
)

// Deploy approval request error variables
var (
	// ErrDeployApprovalRequestActive is returned when an active approval already exists for the same token and commit
	ErrDeployApprovalRequestActive = errors.New(
		"a deploy approval request is already pending or approved for this token and git commit",
	)
	// ErrDeployApprovalRequestNotFound is returned when a deploy approval request cannot be found
	ErrDeployApprovalRequestNotFound = errors.New("deploy approval request not found")
)

// DeployApprovalRequestConflictError wraps an existing approval request when a conflict is detected
type DeployApprovalRequestConflictError struct {
	Request *DeployApprovalRequest
}

func (_ *DeployApprovalRequestConflictError) Error() string {
	return ErrDeployApprovalRequestActive.Error()
}

func (_ *DeployApprovalRequestConflictError) Unwrap() error {
	return ErrDeployApprovalRequestActive
}
