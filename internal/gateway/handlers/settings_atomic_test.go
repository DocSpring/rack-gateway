package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A grouped update with one value of the wrong type changes nothing, and says what's wrong with it.
func TestUpdateSettingsGroupIsAllOrNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, database, _ := setupSettingsHandler(t)
	for _, tc := range []struct {
		update  func(*gin.Context)
		body    string
		problem string
	}{
		{
			handler.UpdateGlobalMFAConfiguration,
			`{"mfa_require_all_users": false, "mfa_trusted_device_ttl_days": 7, "mfa_step_up_window_minutes": 1.5}`,
			"mfa_step_up_window_minutes must be a whole number from 1 to 1440",
		},
		{
			handler.UpdateGlobalDeployApprovals,
			`{"deploy_approvals_enabled": true, "deploy_approval_require_different_approver": "true"}`,
			"deploy_approval_require_different_approver must be true or false",
		},
		{
			handler.UpdateGlobalSessionConfiguration,
			`{"session_timeout_minutes": 100000000}`,
			"session_timeout_minutes must be a whole number from 1 to 1440",
		},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/settings/group", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_email", "admin@example.com")
		tc.update(c)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), tc.problem)
	}
	stored, err := database.GetAllSettings(nil)
	require.NoError(t, err)
	require.Empty(t, stored, "none of the valid values were saved")
}
