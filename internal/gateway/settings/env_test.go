package settings

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// An environment variable overrides a stored value: the deployment config pins the setting.
func TestEnvironmentOverridesTheDatabase(t *testing.T) {
	svc := NewService(dbtest.NewDatabase(t))
	require.NoError(t, svc.SetAppSetting("web", KeyGitHubVerification, false, nil))

	t.Setenv("RGW_APP_WEB_SETTING_GITHUB_VERIFICATION", "true")
	setting, err := svc.GetAppSetting("web", KeyGitHubVerification, true)
	require.NoError(t, err)
	require.Equal(t, true, setting.Value)
	require.Equal(t, SourceEnv, setting.Source)
	require.Equal(t, "RGW_APP_WEB_SETTING_GITHUB_VERIFICATION", setting.EnvVar)

	require.True(t, setting.StoredValueIgnored, "the stored value is reported, so it can be cleared")

	t.Setenv("RGW_APP_WEB_SETTING_VCS_REPO", `"acme/web"`)
	repo, err := svc.GetAppSetting("web", KeyVCSRepo, nil)
	require.NoError(t, err)
	require.Equal(t, "acme/web", repo.Value, "a JSON-encoded string is decoded")

	all, err := svc.GetAllAppSettings("web")
	require.NoError(t, err)
	require.Equal(t, SourceEnv, all[KeyGitHubVerification].Source)
	require.True(t, all[KeyGitHubVerification].StoredValueIgnored)

	shadowed, err := svc.ShadowedSettings()
	require.NoError(t, err)
	require.Equal(t, []string{
		"github_verification (app web), overridden by RGW_APP_WEB_SETTING_GITHUB_VERIFICATION",
	}, shadowed)
}

// Env values come back in the same shapes as stored (JSON-decoded) values.
func TestEnvironmentValuesHaveDatabaseShapes(t *testing.T) {
	svc := NewService(dbtest.NewDatabase(t))
	t.Setenv("RGW_SETTING_DEPLOY_APPROVAL_WINDOW_MINUTES", "30")
	t.Setenv("RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS", "DATABASE_URL, SECRET_KEY")
	t.Setenv("RGW_APP_API_SETTING_PROTECTED_ENV_VARS", `["TOKEN"]`)

	minutes, err := svc.GetDeployApprovalWindowMinutes()
	require.NoError(t, err)
	require.Equal(t, 30, minutes)

	web, err := svc.GetProtectedEnvVars("web")
	require.NoError(t, err)
	require.Equal(t, []string{"DATABASE_URL", "SECRET_KEY"}, web)
	api, err := svc.GetProtectedEnvVars("api")
	require.NoError(t, err)
	require.Equal(t, []string{"TOKEN"}, api)
}

// audit_visible_env_vars is a list of env var names like protected_env_vars: stored or pinned by an environment
// variable, and read back upper-cased without duplicates. By default it is empty, so no values are shown.
func TestAuditVisibleEnvVars(t *testing.T) {
	svc := NewService(dbtest.NewDatabase(t))
	visible, err := svc.GetAuditVisibleEnvVars("web")
	require.NoError(t, err)
	require.Empty(t, visible)

	stored := []string{"port", " PORT ", "Log_Level"}
	require.NoError(t, svc.SetAppSetting("web", KeyAuditVisibleEnvVars, stored, nil))
	visible, err = svc.GetAuditVisibleEnvVars("web")
	require.NoError(t, err)
	require.Equal(t, []string{"PORT", "LOG_LEVEL"}, visible)
	require.ErrorIs(t, svc.SetAppSetting("web", KeyAuditVisibleEnvVars, "PORT", nil), ErrInvalidSettingValue)

	t.Setenv("RGW_APP_WEB_SETTING_AUDIT_VISIBLE_ENV_VARS", "rails_env, PORT")
	visible, err = svc.GetAuditVisibleEnvVars("web")
	require.NoError(t, err)
	require.Equal(t, []string{"RAILS_ENV", "PORT"}, visible)
	require.ErrorIs(t, svc.SetAppSetting("web", KeyAuditVisibleEnvVars, []string{"PORT"}, nil), ErrSetByEnvironment)

	require.NoError(t, ValidateEnvironment([]string{`RGW_APP_API_SETTING_AUDIT_VISIBLE_ENV_VARS=["PORT"]`}))
	require.Error(t, ValidateEnvironment([]string{`RGW_APP_API_SETTING_AUDIT_VISIBLE_ENV_VARS=[PORT]`}))
}

// A setting an env var defines can't be changed through the gateway; storing the value it already has is a no-op.
func TestEnvDefinedSettingsCantBeChanged(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewService(database)
	t.Setenv("RGW_SETTING_MFA_TRUSTED_DEVICE_TTL_DAYS", "45")
	t.Setenv("RGW_APP_WEB_SETTING_DEFAULT_BRANCH", "develop")

	err := svc.SetAppSetting("web", KeyDefaultBranch, "main", nil)
	require.True(t, errors.Is(err, ErrSetByEnvironment), "got %v", err)
	require.ErrorContains(t, err, "RGW_APP_WEB_SETTING_DEFAULT_BRANCH")
	require.NoError(t, svc.SetAppSetting("web", KeyDefaultBranch, "develop", nil))
	_, stored, err := database.GetSetting(stringPtr("web"), KeyDefaultBranch)
	require.NoError(t, err)
	require.False(t, stored, "an unchanged env-defined value isn't stored")

	// A form that submits every MFA field still saves the ones the environment doesn't define.
	require.NoError(t, svc.SetMFASettings(&db.MFASettings{
		RequireAllUsers: false, TrustedDeviceTTLDays: 45, StepUpWindowMinutes: 20,
	}, nil))
	mfa, err := svc.GetMFASettings()
	require.NoError(t, err)
	require.Equal(t, 20, mfa.StepUpWindowMinutes)
	require.Equal(t, 45, mfa.TrustedDeviceTTLDays)

	err = svc.SetMFASettings(&db.MFASettings{TrustedDeviceTTLDays: 7, StepUpWindowMinutes: 20}, nil)
	require.True(t, errors.Is(err, ErrSetByEnvironment), "got %v", err)
}

func TestValidateEnvironment(t *testing.T) {
	require.NoError(t, ValidateEnvironment([]string{
		"PATH=/usr/bin",
		"RGW_SETTING_MFA_REQUIRE_ALL_USERS=true",
		"RGW_SETTING_DEPLOY_APPROVAL_WINDOW_MINUTES=15",
		"RGW_APP_MY_SERVICE_SETTING_PROTECTED_ENV_VARS=A,B",
		`RGW_APP_WEB_SETTING_SERVICE_IMAGE_PATTERNS={"web":"repo:{{GIT_COMMIT}}"}`,
		"RGW_APP_WEB_SETTING_VCS_REPO=acme/web",
		// What a deployment that writes jsonencode(value) for every setting produces (Terraform, for one).
		`RGW_APP_API_SETTING_VCS_REPO="acme/api"`,
		`RGW_APP_API_SETTING_VERIFY_GIT_COMMIT_MODE="latest"`,
		`RGW_APP_API_SETTING_CIRCLECI_APPROVAL_JOB_NAME="approve_deploy"`,
		"RGW_APP_API_SETTING_CIRCLECI_AUTO_APPROVE_ON_APPROVAL=true",
		"RGW_APP_API_SETTING_GITHUB_POST_PR_COMMENT=false",
		"RGW_APP_API_SETTING_APPROVED_DEPLOY_COMMANDS=[]",
		`RGW_APP_API_SETTING_PROTECTED_ENV_VARS=["REDIS_URL","API_HOST"]`,
		`RGW_APP_API_SETTING_SERVICE_IMAGE_PATTERNS={"*":"docker\\.io/acme/api:{{GIT_COMMIT}}-amd64"}`,
		"RGW_SETTING_ALLOW_DESTRUCTIVE_ACTIONS=", // blank: not set
	}))

	invalid := []string{
		"RGW_SETTING_MFA_REQUIRE_ALL_USERS=yes-please",
		"RGW_SETTING_ALLOW_DESTRUCTIVE_ACTIONS=1",
		"RGW_SETTING_DEPLOY_APPROVAL_WINDOW_MINUTES=fifteen",
		"RGW_SETTING_SESSION_TIMEOUT_MINUTES=0",
		"RGW_SETTING_NO_SUCH_SETTING=1",
		"RGW_APP_WEB_SETTING_SERVICE_IMAGE_PATTERNS=not json",
		"RGW_APP_WEB_SETTING_GITHUB_VERIFICATION=1.5",
		"RGW_APP_WEB_SETTING_DEFAULT_BRANCH='main'",
		`RGW_APP_API_SETTING_DEFAULT_BRANCH="main`,
		"RGW_APP_WEB_SETTING_VCS_REPO=null",
		"RGW_APP_WEB_SETTING_VERIFY_GIT_COMMIT_MODE=Latest",
		// Names the gateway never reads: they'd be silently ignored.
		"RGW_SETTING_mfa_step_up_window_minutes=10",
		"RGW_APP_web_SETTING_GITHUB_VERIFICATION=true",
		"RGW_APP_MY-APP_SETTING_GITHUB_VERIFICATION=true",
		"RGW_APP_SETTING_GITHUB_VERIFICATION=true",
	}
	err := ValidateEnvironment(invalid)
	require.Error(t, err)
	for _, entry := range invalid {
		name, _, _ := strings.Cut(entry, "=")
		require.ErrorContains(t, err, name+":")
	}
}

// A quoted list (what Terraform's jsonencode writes for a string, or `- KEY="A,B"` in convox.yml) used to split
// into names with the quotes in them, so it protected nothing. It is refused at startup, as are names that aren't
// env var names, and a running gateway that reads one fails closed.
func TestEnvVarListsRefuseQuotedValuesAndInvalidNames(t *testing.T) {
	invalid := []string{
		`RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS="DATABASE_URL,SECRET_KEY_BASE"`,
		`RGW_APP_API_SETTING_SECRET_ENV_VARS='BILLING_ID'`,
		"RGW_APP_WORKER_SETTING_PROTECTED_ENV_VARS=null",
		"RGW_APP_ADMIN_SETTING_AUDIT_VISIBLE_ENV_VARS=PORT,LOG-LEVEL",
		`RGW_APP_JOBS_SETTING_SECRET_ENV_VARS=["BILLING ID"]`,
		`RGW_APP_CRON_SETTING_APPROVED_DEPLOY_COMMANDS="bin/migrate"`,
		`CONVOX_SECRET_ENV_VARS="DATABASE_URL,REDIS_URL"`,
	}
	err := ValidateEnvironment(invalid)
	require.Error(t, err)
	for _, entry := range invalid {
		name, _, _ := strings.Cut(entry, "=")
		require.ErrorContains(t, err, name+":")
	}

	svc := NewService(dbtest.NewDatabase(t))
	t.Setenv("RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS", `"DATABASE_URL,SECRET_KEY_BASE"`)
	_, err = svc.GetProtectedEnvVars("web")
	require.Error(t, err, "callers treat an error as every key protected")
}

// CONVOX_SECRET_ENV_VARS is parsed like the list settings. A quoted value would split into names with quotes in
// them, which match no key, so it's an error, which callers treat as every key being secret.
func TestSecretEnvVarNames(t *testing.T) {
	env := func(value string) func(string) string {
		return func(name string) string {
			if name == MaskedNamesEnvVar {
				return value
			}
			return ""
		}
	}
	for _, c := range []struct {
		value string
		want  []string
	}{
		{"", nil},
		{" database_url , REDIS_URL ", []string{"DATABASE_URL", "REDIS_URL"}},
		{`["DATABASE_URL","database_url"]`, []string{"DATABASE_URL"}},
	} {
		names, err := SecretEnvVarNames(env(c.value))
		require.NoErrorf(t, err, "value %q", c.value)
		require.Equalf(t, c.want, names, "value %q", c.value)
	}
	for _, value := range []string{`"DATABASE_URL,REDIS_URL"`, `'DATABASE_URL'`, "null", "DATABASE-URL", `["A B"]`} {
		names, err := SecretEnvVarNames(env(value))
		require.ErrorContainsf(t, err, MaskedNamesEnvVar, "value %q", value)
		require.Nil(t, names)
	}
}

// A stored list that isn't a list of env var names (written before validation, or straight to the database) is an
// error, not an empty list: callers then treat every key as secret or protected, and allow no deploy commands.
func TestMalformedStoredListsFailClosed(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewService(database)
	app := "web"
	for _, value := range []interface{}{
		"DATABASE_URL",
		map[string]interface{}{"DATABASE_URL": true},
		[]interface{}{"DATABASE_URL", 1},
		nil,
		[]string{`"DATABASE_URL`},
	} {
		require.NoError(t, database.UpsertSetting(&app, KeyProtectedEnvVars, value, nil))
		names, err := svc.GetProtectedEnvVars(app)
		require.ErrorIsf(t, err, ErrInvalidSettingValue, "stored %#v", value)
		require.Nil(t, names)
	}

	require.NoError(t, database.UpsertSetting(&app, KeyApprovedDeployCommands, "bin/migrate", nil))
	_, err := svc.GetApprovedDeployCommands(app)
	require.ErrorIs(t, err, ErrInvalidSettingValue)
	commands, err := svc.GetApprovedDeployCommands("unset-app")
	require.NoError(t, err)
	require.Empty(t, commands)

	// Image patterns that can't be read refuse the app's builds rather than leave its images unchecked.
	for _, value := range []interface{}{
		"web=repo/web",
		[]interface{}{"repo/web"},
		map[string]interface{}{"web": 1},
		map[string]interface{}{"web": "repo/(web"},
	} {
		require.NoError(t, database.UpsertSetting(&app, KeyServiceImagePatterns, value, nil))
		patterns, err := svc.GetServiceImagePatterns(app)
		require.ErrorIsf(t, err, ErrInvalidSettingValue, "stored %#v", value)
		require.Nil(t, patterns)
	}
	patterns, err := svc.GetServiceImagePatterns("unset-app")
	require.NoError(t, err)
	require.Empty(t, patterns)
}

func stringPtr(s string) *string { return &s }
