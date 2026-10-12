package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const deniedViewerEmail = "viewer@example.com"

// authorizeRouter serves POST /api/v1/users (needs gateway:user:create) and GET /api/v1/settings (human users
// only) behind Authorize, as authUser.
func authorizeRouter(t *testing.T, database *db.Database, logger *audit.Logger, authUser *auth.User) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	manager, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Next()
	})
	router.Use(Authorize(manager, logger))
	router.POST("/api/v1/users", func(c *gin.Context) { c.Status(http.StatusCreated) })
	router.GET("/api/v1/settings", func(c *gin.Context) { c.Status(http.StatusOK) })
	return router
}

func viewerUser(t *testing.T, database *db.Database) *auth.User {
	t.Helper()
	user, err := database.CreateUser(deniedViewerEmail, "Viewer", []string{"viewer"})
	require.NoError(t, err)
	return &auth.User{Email: user.Email, Name: user.Name, Roles: user.Roles, DBUser: user}
}

func deniedEntries(t *testing.T, database *db.Database) []*db.AuditLog {
	t.Helper()
	logs, err := database.GetAuditLogs(deniedViewerEmail, time.Time{}, 20)
	require.NoError(t, err)
	sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })
	return logs
}

// TestGatewayDenialsAreRecordedAndThrottled: a denied gateway API request is recorded, and its repeats within the
// minute are counted in one more entry.
func TestGatewayDenialsAreRecordedAndThrottled(t *testing.T) {
	database := setupTestDatabase(t)
	logger := audit.NewLogger(database)
	router := authorizeRouter(t, database, logger, viewerUser(t, database))

	for i := 0; i < 3; i++ {
		w := serveAudit(router, http.MethodPost, "/api/v1/users")
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Contains(t, w.Body.String(), "insufficient permissions: requires gateway:user:create")
	}

	entries := deniedEntries(t, database)
	require.Len(t, entries, 1, "repeats within the minute are only counted")
	denial := entries[0]
	require.Equal(t, "user.create", denial.Action)
	require.Equal(t, "gateway", denial.ActionType)
	require.Equal(t, audit.StatusDenied, denial.Status)
	require.Equal(t, "deny", denial.RBACDecision)
	require.Equal(t, http.StatusForbidden, denial.HTTPStatus)
	require.Equal(t, 1, denial.EventCount)
	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(denial.Details), &details))
	require.Equal(t, "insufficient permissions: requires gateway:user:create", details["reason"])

	logger.FlushDenials()
	entries = deniedEntries(t, database)
	require.Len(t, entries, 2)
	require.Equal(t, 2, entries[1].EventCount, "the two repeats")
	require.Equal(t, audit.StatusDenied, entries[1].Status)
}

func TestAPITokenDenialsAreRecorded(t *testing.T) {
	database := setupTestDatabase(t)
	logger := audit.NewLogger(database)
	viewer := viewerUser(t, database)
	tokenID := int64(42)
	token := &auth.User{
		Email: viewer.Email, IsAPIToken: true, TokenID: &tokenID, TokenName: "ci", DBUser: viewer.DBUser,
	}
	router := authorizeRouter(t, database, logger, token)

	w := serveAudit(router, http.MethodGet, "/api/v1/settings")
	require.Equal(t, http.StatusForbidden, w.Code)

	entries := deniedEntries(t, database)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].APITokenID)
	require.Equal(t, tokenID, *entries[0].APITokenID)
	require.Equal(t, "ci", entries[0].APITokenName)
	require.Contains(t, entries[0].Details, "API tokens cannot use this endpoint")
	logger.FlushDenials()
}

// TestDenialsFailOpen: the caller gets the same 403 when the audit log can't record the denial.
func TestDenialsFailOpen(t *testing.T) {
	database := setupTestDatabase(t)
	router := authorizeRouter(t, database, unavailableLogger(), viewerUser(t, database))

	w := serveAudit(router, http.MethodPost, "/api/v1/users")
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "insufficient permissions: requires gateway:user:create")
}
