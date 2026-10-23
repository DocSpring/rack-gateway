package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
)

// settingChanges returns the "changes" recorded in the result entry of each settings request, oldest first.
func settingChanges(t *testing.T, database *db.Database, email string) []map[string]map[string]interface{} {
	t.Helper()
	logs, err := database.GetAuditLogs(email, time.Time{}, 20)
	require.NoError(t, err)
	sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })

	var changes []map[string]map[string]interface{}
	for _, entry := range logs {
		if entry.Status == audit.StatusAttempted {
			continue
		}
		var details struct {
			Changes map[string]map[string]interface{} `json:"changes"`
		}
		require.NoError(t, json.Unmarshal([]byte(entry.Details), &details), entry.Details)
		changes = append(changes, details.Changes)
	}
	return changes
}

// Each settings change records every key's value before and after it, so the audit log shows what changed and
// what it was changed back to.
func TestSettingsChangesRecordBeforeAndAfterValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, database, _ := setupSettingsHandler(t)
	authUser := &auth.User{Email: "admin@example.com", Name: "Admin"}

	const route = "/api/v1/settings/deploy-approvals"
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Next()
	})
	router.Use(middleware.AuditBeforeActing(audit.NewLogger(database), database))
	router.PUT(route, handler.UpdateGlobalDeployApprovals)
	router.DELETE(route, handler.DeleteGlobalDeployApprovals)

	body, err := json.Marshal(map[string]interface{}{"deploy_approvals_enabled": false})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, route, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, route+"?key=deploy_approvals_enabled", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	changes := settingChanges(t, database, authUser.Email)
	require.Len(t, changes, 2)
	require.Equal(t, map[string]map[string]interface{}{
		"deploy_approvals_enabled": {"from": true, "to": false},
	}, changes[0])
	require.Equal(t, map[string]map[string]interface{}{
		"deploy_approvals_enabled": {"from": false, "to": true},
	}, changes[1])
}
