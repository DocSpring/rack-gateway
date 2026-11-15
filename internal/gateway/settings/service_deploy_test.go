package settings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Auto-approval's two settings come from one read: off by default, the stored values when saved, an environment
// variable over a stored value, and an error for a stored value that doesn't fit.
func TestGetDeployApprovalAutoApproval(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewService(database)

	got, err := svc.GetDeployApprovalAutoApproval()
	require.NoError(t, err)
	require.Equal(t, DeployApprovalAutoApproval{}, got)

	require.NoError(t, svc.SetGlobalSettings(map[string]interface{}{
		KeyDeployApprovalAutoApprove:              true,
		KeyDeployApprovalRequireDifferentApprover: true,
	}, nil))
	got, err = svc.GetDeployApprovalAutoApproval()
	require.NoError(t, err)
	require.Equal(t, DeployApprovalAutoApproval{Enabled: true, RequireDifferentApprover: true}, got)

	t.Setenv("RGW_SETTING_DEPLOY_APPROVAL_REQUIRE_DIFFERENT_APPROVER", "false")
	got, err = svc.GetDeployApprovalAutoApproval()
	require.NoError(t, err)
	require.Equal(t, DeployApprovalAutoApproval{Enabled: true}, got)

	require.NoError(t, database.UpsertSetting(nil, KeyDeployApprovalAutoApprove, "yes", nil))
	_, err = svc.GetDeployApprovalAutoApproval()
	require.ErrorIs(t, err, ErrInvalidSettingValue)
}
