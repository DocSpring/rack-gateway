package middleware

import (
	"context"
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
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

const auditAdminEmail = "admin@example.com"

// auditRoutes are real gateway routes: a write, a read, an MFA step (a fail-open write) and the env reveal route.
var auditRoutes = []struct{ method, pattern string }{
	{http.MethodPost, "/api/v1/users"},
	{http.MethodGet, "/api/v1/users"},
	{http.MethodPost, "/api/v1/auth/mfa/verify"},
	{http.MethodGet, "/api/v1/apps/:app/env"},
}

// auditRouter serves auditRoutes with handler behind AuditBeforeActing, as an authenticated admin.
func auditRouter(logger *audit.Logger, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authUser := &auth.User{Email: auditAdminEmail, Name: "Admin"}
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Next()
	})
	router.Use(AuditBeforeActing(logger))
	for _, route := range auditRoutes {
		router.Handle(route.method, route.pattern, handler)
	}
	return router
}

func serveAudit(router *gin.Engine, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func unavailableLogger() *audit.Logger {
	logger := audit.NewLogger(nil)
	logger.SetChainWriter(audittest.FailingChainWriter{})
	return logger
}

func TestFailClosedRoutesAreRefusedWhenTheAuditLogIsDown(t *testing.T) {
	called := false
	router := auditRouter(unavailableLogger(), func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})

	w := serveAudit(router, http.MethodPost, "/api/v1/users")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "audit log unavailable; refusing to perform user.create")

	w = serveAudit(router, http.MethodGet, "/api/v1/apps/web/env?secrets=true")
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "revealing secret values fails closed")
	require.Contains(t, w.Body.String(), "refusing to perform secret.read")

	require.False(t, called, "a refused request never reaches its handler")
}

// There are no fail-open routes: reads, MFA verification and plain env reads are refused like changes.
func TestNoRouteRunsWhenTheAuditLogIsDown(t *testing.T) {
	called := false
	router := auditRouter(unavailableLogger(), func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPost, "/api/v1/auth/mfa/verify"},
		{http.MethodGet, "/api/v1/apps/web/env"},
		{http.MethodGet, "/api/v1/apps/web/env?key=PORT"},
	} {
		w := serveAudit(router, target.method, target.path)
		require.Equalf(t, http.StatusServiceUnavailable, w.Code, "%s %s", target.method, target.path)
	}
	require.False(t, called, "a refused request never reaches its handler")
}

// Without an audit logger there's nowhere to record a request, so it's refused.
func TestRequestsAreRefusedWithoutAnAuditLogger(t *testing.T) {
	router := auditRouter(nil, func(c *gin.Context) { c.Status(http.StatusOK) })
	require.Equal(t, http.StatusServiceUnavailable, serveAudit(router, http.MethodGet, "/api/v1/users").Code)
}

// requestEntries returns the admin's audit entries, oldest first.
func requestEntries(t *testing.T, database *db.Database) []*db.AuditLog {
	t.Helper()
	logs, err := database.GetAuditLogs(auditAdminEmail, time.Time{}, 20)
	require.NoError(t, err)
	sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })
	return logs
}

// TestAttemptIsPairedWithTheHandlersEntry: the handler's own entry is the attempt's result, so no generic
// result is added.
func TestAttemptIsPairedWithTheHandlersEntry(t *testing.T) {
	database := setupTestDatabase(t)
	logger := audit.NewLogger(database)
	router := auditRouter(logger, func(c *gin.Context) {
		require.NoError(t, logger.LogRequestEntry(c.Request.Context(), &db.AuditLog{
			UserEmail: auditAdminEmail, ActionType: "users", Action: "user.create", Resource: "new@example.com",
			Status: audit.StatusSuccess, HTTPStatus: http.StatusCreated,
		}))
		c.Status(http.StatusCreated)
	})

	w := serveAudit(router, http.MethodPost, "/api/v1/users")
	require.Equal(t, http.StatusCreated, w.Code)

	entries := requestEntries(t, database)
	require.Len(t, entries, 2)
	require.Equal(t, audit.StatusAttempted, entries[0].Status)
	require.Equal(t, "user.create", entries[0].Action)
	require.Equal(t, "/users", entries[0].Resource)
	require.Equal(t, audit.StatusSuccess, entries[1].Status)
	require.Equal(t, "new@example.com", entries[1].Resource)
	require.NotEmpty(t, entries[0].RequestID)
	require.Equal(t, entries[0].RequestID, entries[1].RequestID)
}

// TestAttemptWithoutAHandlerEntryGetsAResult: when the handler writes no entry, the request's outcome is
// recorded for it.
func TestAttemptWithoutAHandlerEntryGetsAResult(t *testing.T) {
	database := setupTestDatabase(t)
	router := auditRouter(audit.NewLogger(database), func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
	})

	w := serveAudit(router, http.MethodPost, "/api/v1/users")
	require.Equal(t, http.StatusBadRequest, w.Code)

	entries := requestEntries(t, database)
	require.Len(t, entries, 2)
	require.Equal(t, audit.StatusAttempted, entries[0].Status)
	require.Equal(t, audit.StatusFailed, entries[1].Status)
	require.Equal(t, http.StatusBadRequest, entries[1].HTTPStatus)
	require.Equal(t, "user.create", entries[1].Action)
	require.Equal(t, entries[0].RequestID, entries[1].RequestID)
}

// A read is recorded like any other request: its attempt before the handler runs, and its result after.
func TestReadsAreRecordedAsAttempts(t *testing.T) {
	database := setupTestDatabase(t)
	router := auditRouter(audit.NewLogger(database), func(c *gin.Context) { c.Status(http.StatusOK) })

	require.Equal(t, http.StatusOK, serveAudit(router, http.MethodGet, "/api/v1/users").Code)
	entries := requestEntries(t, database)
	require.Len(t, entries, 2)
	require.Equal(t, audit.StatusAttempted, entries[0].Status)
	require.Equal(t, "user.list", entries[0].Action)
	require.Equal(t, audit.StatusSuccess, entries[1].Status)
	require.Equal(t, entries[0].RequestID, entries[1].RequestID)
}
