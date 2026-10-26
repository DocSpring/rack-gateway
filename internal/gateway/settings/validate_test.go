package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCollection(t *testing.T) {
	valid := map[string]interface{}{
		KeyAuditVisibleEnvVars:           []interface{}{"PORT"},
		KeyProtectedEnvVars:              []string{"DATABASE_URL"},
		AppSettingSecretEnvVars.String(): []interface{}{},
		KeyServiceImagePatterns: map[string]interface{}{
			"*": `docker\.io/docspringcom/app:{{GIT_COMMIT}}-amd64`,
		},
		"some_other_key": map[string]interface{}{"anything": 1},
	}
	for key, value := range valid {
		require.NoErrorf(t, validateCollection(key, value), "key %s", key)
	}

	invalid := map[string]interface{}{
		KeyAuditVisibleEnvVars:           map[string]interface{}{"PORT": true},
		KeyProtectedEnvVars:              "DATABASE_URL",
		AppSettingSecretEnvVars.String(): []interface{}{"A", 1},
		KeyServiceImagePatterns:          []interface{}{"docker.io/app:{{GIT_COMMIT}}"},
	}
	for key, value := range invalid {
		require.ErrorIsf(t, validateCollection(key, value), ErrInvalidSettingValue, "key %s", key)
	}

	require.ErrorIs(t, validateCollection(KeyServiceImagePatterns, map[string]interface{}{"web": "app:("}),
		ErrInvalidSettingValue)
	require.ErrorIs(t, validateCollection(KeyServiceImagePatterns, map[string]interface{}{"web": 1}),
		ErrInvalidSettingValue)
}

// The env var lists must hold env var names: a name with quotes or other characters in it matches no key, so the
// list would quietly protect, mask or show nothing.
func TestValidateCollectionRefusesInvalidEnvVarNames(t *testing.T) {
	for _, key := range []string{KeyProtectedEnvVars, AppSettingSecretEnvVars.String(), KeyAuditVisibleEnvVars} {
		require.NoErrorf(t, validateCollection(key, []interface{}{"DATABASE_URL", " port ", "_X1"}), "key %s", key)
		for _, names := range [][]string{
			{`"DATABASE_URL`, `SECRET_KEY_BASE"`}, // a quoted comma-separated value split on its comma
			{"DATABASE-URL"},
			{"1PASSWORD"},
			{"A B"},
			{""},
		} {
			err := validateCollection(key, names)
			require.ErrorIsf(t, err, ErrInvalidSettingValue, "key %s, names %q", key, names)
		}
	}
}

// Each setting takes only values of its own type: a boolean sent as a string, a fractional or out-of-range number,
// or a value a setting doesn't allow is refused before it's saved.
func TestValidateSettingValue(t *testing.T) {
	web := "web"
	type value struct {
		app   *string
		key   string
		value interface{}
	}
	for _, tc := range []value{
		{nil, KeyDeployApprovalRequireDifferentApprover, true},
		{nil, KeySessionTimeoutMinutes, float64(1)},
		{nil, KeySessionTimeoutMinutes, 1440},
		{nil, GlobalSettingStepUpWindowMinutes.String(), float64(30)},
		{nil, KeyDefaultCIProvider, "circleci"},
		{&web, KeyVerifyGitCommitMode, VerifyGitCommitModeBranch},
		{&web, KeyVerifyGitCommitMode, nil},
		{&web, KeyVCSRepo, nil},
		{&web, KeyGitHubVerification, false},
	} {
		require.NoErrorf(t, ValidateSettingValue(tc.app, tc.key, tc.value), "%s = %#v", tc.key, tc.value)
	}
	for _, tc := range []value{
		{nil, KeyDeployApprovalRequireDifferentApprover, "true"},
		{nil, KeyMFARequireAllUsers, nil},
		{nil, KeySessionTimeoutMinutes, float64(0)},
		{nil, KeySessionTimeoutMinutes, float64(1441)},
		{nil, KeySessionTimeoutMinutes, 1.5},
		{nil, KeySessionTimeoutMinutes, "5"},
		{nil, KeyTrustedDeviceTTLDays, float64(366)},
		{nil, KeyDeployApprovalWindowMinutes, float64(-15)},
		{nil, KeyDefaultCIProvider, float64(1)},
		{&web, KeyVerifyGitCommitMode, "newest"},
		{&web, KeyGitHubVerification, "false"},
		{&web, KeyProtectedEnvVars, "DATABASE_URL"},
		{nil, KeyProtectedEnvVars, []string{"X"}},    // an app setting, not a global one
		{&web, KeySessionTimeoutMinutes, float64(5)}, // a global setting, not an app one
		{nil, "no_such_setting", true},
	} {
		require.ErrorIsf(t, ValidateSettingValue(tc.app, tc.key, tc.value), ErrInvalidSettingValue,
			"%s = %#v", tc.key, tc.value)
	}
}

// Every default passes its own setting's check, so each numeric setting's maximum is above its default.
func TestEverySettingDefaultIsValid(t *testing.T) {
	web := "web"
	for key, value := range DefaultGlobalSettings {
		require.NoErrorf(t, ValidateSettingValue(nil, key, value), "global %s", key)
	}
	for key, value := range DefaultAppSettings {
		require.NoErrorf(t, ValidateSettingValue(&web, key, value), "app %s", key)
	}
}
