package settings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestGetAppSetting_GlobalDefaultFallback(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { _ = database.Close() })

	svc := NewService(database)

	t.Run("ci_provider falls back to global default when app setting is nil", func(t *testing.T) {
		// Set global default to "circleci"
		err := svc.SetGlobalSetting(KeyDefaultCIProvider, "circleci", nil)
		require.NoError(t, err)

		// Don't set app-specific ci_provider (it should be nil by default)

		// GetAppSetting should return the global default
		setting, err := svc.GetAppSetting("docspring", KeyCIProvider, "")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "circleci", setting.Value)
		require.Equal(t, SourceGlobalDefault, setting.Source)
	})

	t.Run("ci_provider falls back to global default when app setting is empty string", func(t *testing.T) {
		// Set global default to "circleci"
		err := svc.SetGlobalSetting(KeyDefaultCIProvider, "circleci", nil)
		require.NoError(t, err)

		// Set app setting to empty string
		err = svc.SetAppSetting("docspring", KeyCIProvider, "", nil)
		require.NoError(t, err)

		// GetAppSetting should return the global default
		setting, err := svc.GetAppSetting("docspring", KeyCIProvider, "")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "circleci", setting.Value)
		require.Equal(t, SourceGlobalDefault, setting.Source)
	})

	t.Run("ci_provider uses app-specific value when set", func(t *testing.T) {
		// Set global default to "circleci"
		err := svc.SetGlobalSetting(KeyDefaultCIProvider, "circleci", nil)
		require.NoError(t, err)

		// Set app setting to "github_actions"
		err = svc.SetAppSetting("docspring", KeyCIProvider, "github_actions", nil)
		require.NoError(t, err)

		// GetAppSetting should return the app-specific value
		setting, err := svc.GetAppSetting("docspring", KeyCIProvider, "")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "github_actions", setting.Value)
		require.Equal(t, SourceDB, setting.Source)
	})

	t.Run("vcs_provider falls back to global default when app setting is nil", func(t *testing.T) {
		// Set global default to "github"
		err := svc.SetGlobalSetting(KeyDefaultVCSProvider, "github", nil)
		require.NoError(t, err)

		// Don't set app-specific vcs_provider

		// GetAppSetting should return the global default
		setting, err := svc.GetAppSetting("myapp", KeyVCSProvider, "")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "github", setting.Value)
		require.Equal(t, SourceGlobalDefault, setting.Source)
	})

	t.Run("other app settings do not fall back to global defaults", func(t *testing.T) {
		// Set a non-VCS/CI setting - should not fall back to any global default
		setting, err := svc.GetAppSetting("docspring", KeyCircleCIApprovalJobName, "default-job")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "default-job", setting.Value)
		require.Equal(t, SourceDefault, setting.Source)
	})
}

func TestGetAllAppSettings_GlobalDefaultFallback(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { _ = database.Close() })

	svc := NewService(database)

	t.Run("GetAllAppSettings includes global defaults for ci_provider and vcs_provider", func(t *testing.T) {
		// Set global defaults
		err := svc.SetGlobalSetting(KeyDefaultCIProvider, "circleci", nil)
		require.NoError(t, err)
		err = svc.SetGlobalSetting(KeyDefaultVCSProvider, "github", nil)
		require.NoError(t, err)

		// Don't set app-specific values

		settings, err := svc.GetAllAppSettings("docspring")
		require.NoError(t, err)

		ciProvider := settings[KeyCIProvider]
		require.NotNil(t, ciProvider)
		require.Equal(t, "circleci", ciProvider.Value)
		require.Equal(t, SourceGlobalDefault, ciProvider.Source)

		vcsProvider := settings[KeyVCSProvider]
		require.NotNil(t, vcsProvider)
		require.Equal(t, "github", vcsProvider.Value)
		require.Equal(t, SourceGlobalDefault, vcsProvider.Source)
	})
}

func TestGetAppSetting_HardcodedGlobalDefaultFallback(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { _ = database.Close() })

	svc := NewService(database)

	// This test verifies the bug fix: when there's no global default in the DB,
	// GetAppSetting should fall back to the hardcoded default from DefaultGlobalSettings.
	// Previously it would pass "" as the default, returning empty string instead of "circleci".

	t.Run("ci_provider falls back to hardcoded default when no DB entries exist", func(t *testing.T) {
		// DO NOT set any global default in the database
		// DO NOT set any app-specific ci_provider

		// GetAppSetting should return the hardcoded default from DefaultGlobalSettings
		setting, err := svc.GetAppSetting("newapp", KeyCIProvider, "")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "circleci", setting.Value, "should fall back to hardcoded default_ci_provider")
		require.Equal(t, SourceGlobalDefault, setting.Source)
	})

	t.Run("vcs_provider falls back to hardcoded default when no DB entries exist", func(t *testing.T) {
		// DO NOT set any global default in the database
		// DO NOT set any app-specific vcs_provider

		// GetAppSetting should return the hardcoded default from DefaultGlobalSettings
		setting, err := svc.GetAppSetting("newapp", KeyVCSProvider, "")
		require.NoError(t, err)
		require.NotNil(t, setting)
		require.Equal(t, "github", setting.Value, "should fall back to hardcoded default_vcs_provider")
		require.Equal(t, SourceGlobalDefault, setting.Source)
	})
}
