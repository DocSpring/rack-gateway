package jobslack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test AuditEventArgs.Kind
func TestAuditEventArgs_Kind(t *testing.T) {
	args := AuditEventArgs{AuditLogID: 123}
	assert.Equal(t, "slack:audit_event", args.Kind())
}

// Test NewAuditEventWorker
func TestNewAuditEventWorker(t *testing.T) {
	worker := NewAuditEventWorker(nil, nil)
	require.NotNil(t, worker)
}

// Test DeployApprovalArgs.Kind
func TestDeployApprovalArgs_Kind(t *testing.T) {
	args := DeployApprovalArgs{
		DeployApprovalRequestID: 456,
		GatewayDomain:           "gateway.example.com",
	}
	assert.Equal(t, "slack:deploy_approval", args.Kind())
}

// Test NewDeployApprovalWorker
func TestNewDeployApprovalWorker(t *testing.T) {
	worker := NewDeployApprovalWorker(nil, nil)
	require.NotNil(t, worker)
}
