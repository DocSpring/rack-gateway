package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupSettingsHandler(t *testing.T) (*SettingsHandler, *db.Database, rbac.RBACManager) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	settingsService := settings.NewService(database)
	handler := NewSettingsHandler(settingsService, mgr)

	return handler, database, mgr
}

func TestUpdateGlobalSetting_Boolean(t *testing.T) {
	handler, database, mgr := setupSettingsHandler(t)

	// Create a test user
	require.NoError(t, mgr.SaveUser("admin@example.com", &rbac.UserConfig{
		Name:  "Admin",
		Roles: []string{"admin"},
	}))

	tests := []struct {
		name           string
		key            string
		value          interface{}
		expectedStatus int
		expectedValue  interface{}
		expectedSource settings.SettingSource
	}{
		{
			name:           "set boolean to true",
			key:            "allow_destructive_actions",
			value:          true,
			expectedStatus: http.StatusOK,
			expectedValue:  true,
			expectedSource: settings.SourceDB,
		},
		{
			name:           "set boolean to false",
			key:            "allow_destructive_actions",
			value:          false,
			expectedStatus: http.StatusOK,
			expectedValue:  false,
			expectedSource: settings.SourceDB,
		},
		{
			name:           "set number",
			key:            "mfa_trusted_device_ttl_days",
			value:          float64(60), // JSON numbers are float64
			expectedStatus: http.StatusOK,
			expectedValue:  float64(60),
			expectedSource: settings.SourceDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any previous setting
			_ = database.DeleteSetting(nil, tt.key)

			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			body, err := json.Marshal(tt.value)
			require.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPut, "/admin/settings/"+tt.key, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("user_email", "admin@example.com")
			c.Params = gin.Params{{Key: "key", Value: tt.key}}

			handler.UpdateGlobalSetting(c)

			require.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var result settings.Setting
				err := json.Unmarshal(w.Body.Bytes(), &result)
				require.NoError(t, err, "Response body: %s", w.Body.String())
				require.Equal(t, tt.expectedValue, result.Value, "Response body: %s", w.Body.String())
				require.Equal(t, tt.expectedSource, result.Source, "Response body: %s", w.Body.String())
			}
		})
	}

	// Test clearing a setting (reverting to default)
	t.Run("clear setting reverts to default", func(t *testing.T) {
		// First set a value
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		c.Request = httptest.NewRequest(http.MethodPut, "/admin/settings/allow_destructive_actions", bytes.NewReader([]byte("true")))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_email", "admin@example.com")
		c.Params = gin.Params{{Key: "key", Value: "allow_destructive_actions"}}

		handler.UpdateGlobalSetting(c)
		require.Equal(t, http.StatusOK, w.Code)

		// Verify it's in DB
		valueBytes, exists, err := database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.True(t, exists)
		require.NotNil(t, valueBytes)

		// Now clear it with DELETE
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/settings/allow_destructive_actions", nil)
		c.Set("user_email", "admin@example.com")
		c.Params = gin.Params{{Key: "key", Value: "allow_destructive_actions"}}

		handler.DeleteGlobalSetting(c)
		require.Equal(t, http.StatusOK, w.Code)

		// Verify it's deleted from DB
		_, exists, err = database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.False(t, exists)

		// Response should show default value with source "default"
		var result settings.Setting
		err = json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		require.Equal(t, false, result.Value) // default is false
		require.Equal(t, settings.SourceDefault, result.Source)
	})
}
