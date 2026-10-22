package settings

import (
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A group of settings saves together: with one bad value, none of them change. A group is deleted together too.
func TestSetSettingsSavesAllOrNone(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewService(database)
	err := svc.SetGlobalSettings(map[string]interface{}{
		KeyTrustedDeviceTTLDays:                   7,
		GlobalSettingStepUpWindowMinutes.String(): "20",
	}, nil)
	require.ErrorIs(t, err, ErrInvalidSettingValue)
	_, stored, err := database.GetSetting(nil, KeyTrustedDeviceTTLDays)
	require.NoError(t, err)
	require.False(t, stored, "the valid value in the group isn't saved either")

	require.NoError(t, svc.SetAppSettings("web", map[string]interface{}{
		KeyVCSRepo: "acme/web", KeyGitHubVerification: false, KeyDefaultBranch: "trunk",
	}, nil))
	require.NoError(t, svc.DeleteAppSettings("web", []string{KeyVCSRepo, KeyGitHubVerification}))
	remaining, err := database.GetAllSettings(stringPtr("web"))
	require.NoError(t, err)
	require.Equal(t, []string{KeyDefaultBranch}, slices.Collect(maps.Keys(remaining)))
}

// Two saves of the same group at once each apply whole: the result is one of them, never a mix of both.
func TestConcurrentGroupSavesDontInterleave(t *testing.T) {
	svc := NewService(dbtest.NewDatabase(t))
	stepUp := GlobalSettingStepUpWindowMinutes.String()
	groups := []map[string]interface{}{
		{KeyMFARequireAllUsers: true, KeyTrustedDeviceTTLDays: 7, stepUp: 5},
		{KeyMFARequireAllUsers: false, KeyTrustedDeviceTTLDays: 30, stepUp: 15},
	}
	for range 10 {
		var wg sync.WaitGroup
		errs := make(chan error, len(groups))
		for _, group := range groups {
			wg.Go(func() { errs <- svc.SetGlobalSettings(group, nil) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		mfa, err := svc.GetMFASettings()
		require.NoError(t, err)
		require.Contains(t, groups, map[string]interface{}{
			KeyMFARequireAllUsers: mfa.RequireAllUsers, KeyTrustedDeviceTTLDays: mfa.TrustedDeviceTTLDays,
			stepUp: mfa.StepUpWindowMinutes,
		})
	}
}

// A stored value that doesn't fit its setting (one saved before values were checked) is an error rather than some
// other value, and the callers fail safe. Saving a valid value replaces it.
func TestInvalidStoredValuesAreErrors(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewService(database)
	require.NoError(t, database.UpsertSetting(nil, KeyDeployApprovalRequireDifferentApprover, "true", nil))
	_, err := svc.GetDeployApprovalRequireDifferentApprover()
	require.ErrorIs(t, err, ErrInvalidSettingValue)
	require.ErrorContains(t, err, "RGW_SETTING_DEPLOY_APPROVAL_REQUIRE_DIFFERENT_APPROVER")

	require.NoError(t, database.UpsertSetting(nil, KeySessionTimeoutMinutes, 1.5, nil))
	minutes, err := svc.GetSessionTimeoutMinutes()
	require.ErrorIs(t, err, ErrInvalidSettingValue)
	require.Equal(t, 5, minutes, "the default comes with the error")
	require.NoError(t, database.UpsertSetting(nil, KeySessionTimeoutMinutes, "1", nil))
	_, err = svc.GetSessionTimeoutDuration()
	require.ErrorIs(t, err, ErrInvalidSettingValue)

	stepUp := GlobalSettingStepUpWindowMinutes.String()
	require.NoError(t, database.UpsertSetting(nil, stepUp, 1e12, nil))
	_, err = svc.GetMFASettings()
	require.ErrorIs(t, err, ErrInvalidSettingValue)
	require.NoError(t, svc.SetGlobalSettings(map[string]interface{}{stepUp: 15}, nil))
	mfa, err := svc.GetMFASettings()
	require.NoError(t, err)
	require.Equal(t, 15, mfa.StepUpWindowMinutes)

	require.Error(t, ValidateEnvironment([]string{"RGW_SETTING_SESSION_TIMEOUT_MINUTES=1441"}),
		"an environment variable gets the same range")
}
