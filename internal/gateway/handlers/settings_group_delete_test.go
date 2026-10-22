package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func deleteVCSCIDeploy(t *testing.T, handler *SettingsHandler, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/apps/web/settings/vcs-ci-deploy"+query, nil)
	c.Params = gin.Params{{Key: "app", Value: "web"}}
	c.Set("user_email", "admin@example.com")
	handler.DeleteAppVCSCIDeploySettings(c)
	return w
}

// The VCS & CI card's Clear button deletes the group with no keys: that clears every key in the group.
func TestDeleteAppSettingsGroupWithoutKeysClearsTheGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _, _ := setupSettingsHandler(t)
	svc := handler.settingsService
	require.NoError(t, svc.SetAppSettings("web", map[string]interface{}{
		settings.KeyVCSRepo:            "acme/web",
		settings.KeyGitHubVerification: false,
		settings.KeyProtectedEnvVars:   []string{"SECRET"},
	}, nil))

	w := deleteVCSCIDeploy(t, handler, "?key="+settings.KeyVCSRepo)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	verification, err := svc.GetAppSetting("web", settings.KeyGitHubVerification, nil)
	require.NoError(t, err)
	require.Equal(t, settings.SourceDB, verification.Source, "only the named key is cleared")

	w = deleteVCSCIDeploy(t, handler, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, key := range []string{settings.KeyVCSRepo, settings.KeyGitHubVerification} {
		setting, err := svc.GetAppSetting("web", key, nil)
		require.NoError(t, err)
		require.NotEqual(t, settings.SourceDB, setting.Source, key)
	}
	protected, err := svc.GetAppSetting("web", settings.KeyProtectedEnvVars, nil)
	require.NoError(t, err)
	require.Equal(t, settings.SourceDB, protected.Source, "keys outside the group are untouched")

	w = deleteVCSCIDeploy(t, handler, "?key="+settings.KeyProtectedEnvVars)
	require.Equal(t, http.StatusBadRequest, w.Code, "a key outside the group is refused")
}
