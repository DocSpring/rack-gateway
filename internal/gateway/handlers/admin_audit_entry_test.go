package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
)

func getAuditLog(handler *handlers.AdminHandler, id string) (int, string) {
	c, w := newGinContext(http.MethodGet, "/api/v1/audit-logs/"+id, nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	attachAdminContext(c)
	handler.GetAuditLog(c)
	return w.Code, w.Body.String()
}

// An audit event can be read by its ID, which Slack messages link to. An ID that isn't one is refused, and one
// with no event isn't found.
func TestGetAuditLog(t *testing.T) {
	handler, database, _ := newAdminHandler(t)
	entry := &db.AuditLog{
		UserEmail: "admin@example.com", ActionType: "convox", Action: "process.exec.start",
		Resource: "p-1", Details: `{"command":"ls","justification":"List the files for a support ticket"}`,
		Status: "success", RBACDecision: "allow", HTTPStatus: http.StatusSwitchingProtocols,
	}
	require.NoError(t, database.CreateAuditLog(entry))

	status, body := getAuditLog(handler, strconv.FormatInt(entry.ID, 10))
	require.Equal(t, http.StatusOK, status, body)
	var got db.AuditLog
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.Equal(t, entry.ID, got.ID)
	require.Equal(t, "process.exec.start", got.Action)
	require.Equal(t, entry.Details, got.Details)

	status, body = getAuditLog(handler, strconv.FormatInt(entry.ID+1000, 10))
	require.Equal(t, http.StatusNotFound, status, body)
	for _, invalid := range []string{"abc", "0", "-1", "1.5"} {
		status, body = getAuditLog(handler, invalid)
		require.Equal(t, http.StatusBadRequest, status, invalid+": "+body)
	}
}
