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

func TestUpdateGlobalSettings_Boolean(t *testing.T) {
	handler, database, mgr := setupSettingsHandler(t)

	// Create a test user
	require.NoError(t, mgr.SaveUser("admin@example.com", &rbac.UserConfig{
		Name:  "Admin",
		Roles: []string{"admin"},
	}))

	tests := []struct {
		name           string
		updates        map[string]interface{}
		expectedStatus int
		expectedValues map[string]interface{}
		expectedSource settings.SettingSource
	}{
		{
			name: "set single boolean to true",
			updates: map[string]interface{}{
				"allow_destructive_actions": true,
			},
			expectedStatus: http.StatusOK,
			expectedValues: map[string]interface{}{
				"allow_destructive_actions": true,
			},
			expectedSource: settings.SourceDB,
		},
		{
			name: "set multiple settings",
			updates: map[string]interface{}{
				"allow_destructive_actions":   false,
				"mfa_trusted_device_ttl_days": float64(60), // JSON numbers are float64
			},
			expectedStatus: http.StatusOK,
			expectedValues: map[string]interface{}{
				"allow_destructive_actions":   false,
				"mfa_trusted_device_ttl_days": float64(60),
			},
			expectedSource: settings.SourceDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any previous settings
			for key := range tt.updates {
				_ = database.DeleteSetting(nil, key)
			}

			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			body, err := json.Marshal(tt.updates)
			require.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("user_email", "admin@example.com")

			handler.UpdateGlobalSettings(c)

			require.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var result map[string]settings.Setting
				err := json.Unmarshal(w.Body.Bytes(), &result)
				require.NoError(t, err, "Response body: %s", w.Body.String())

				for key, expectedValue := range tt.expectedValues {
					setting, ok := result[key]
					require.True(t, ok, "Expected key %s in response", key)
					require.Equal(t, expectedValue, setting.Value, "Response body: %s", w.Body.String())
					require.Equal(t, tt.expectedSource, setting.Source, "Response body: %s", w.Body.String())
				}
			}
		})
	}

	// Test clearing settings (reverting to default)
	t.Run("clear settings reverts to default", func(t *testing.T) {
		// First set values
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		c.Request = httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_email", "admin@example.com")

		handler.UpdateGlobalSettings(c)
		require.Equal(t, http.StatusOK, w.Code)

		// Verify it's in DB
		valueBytes, exists, err := database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.True(t, exists)
		require.NotNil(t, valueBytes)

		// Now clear it with DELETE
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/settings?key=allow_destructive_actions", nil)
		c.Set("user_email", "admin@example.com")

		handler.DeleteGlobalSettings(c)
		require.Equal(t, http.StatusOK, w.Code)

		// Verify it's deleted from DB
		_, exists, err = database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.False(t, exists)

		// Response should show default value with source "default"
		var result map[string]settings.Setting
		err = json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		setting := result["allow_destructive_actions"]
		require.Equal(t, false, setting.Value) // default is false
		require.Equal(t, settings.SourceDefault, setting.Source)
	})
}
