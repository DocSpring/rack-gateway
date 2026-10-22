package middleware

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// requestLines serves each request through router and returns the request log lines written to stdout.
func requestLines(t *testing.T, router *gin.Engine, requests ...*http.Request) []audit.LogEntry {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = writer
	output := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(reader)
		output <- data
	}()
	for _, req := range requests {
		router.ServeHTTP(httptest.NewRecorder(), req)
	}
	os.Stdout = original
	require.NoError(t, writer.Close())

	var entries []audit.LogEntry
	scanner := bufio.NewScanner(bytes.NewReader(<-output))
	for scanner.Scan() {
		var entry audit.LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Path != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// A rack proxy request refused before it reached the proxy (here, without credentials) is logged, as a denial.
// One the proxy logged itself, on a request it derived, isn't logged again.
func TestRequestLoggerLogsRackProxyRequestsTheProxyDidntLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := audit.NewLogger(nil)
	router := gin.New()
	router.Use(RequestLogger(logger, "staging", false))
	proxied := router.Group("/api/v1/rack-proxy")
	proxied.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "CLI authentication required"})
		}
	})
	proxied.GET("/*path", func(c *gin.Context) {
		derived := c.Request.Clone(context.WithValue(c.Request.Context(), contextKey("derived"), true))
		logger.LogRequest(derived, "dev@example.com", "staging", "allow", http.StatusOK, time.Millisecond, nil)
		c.Status(http.StatusOK)
	})

	refused := httptest.NewRequest(http.MethodGet, "/api/v1/rack-proxy/apps", nil)
	forwarded := httptest.NewRequest(http.MethodGet, "/api/v1/rack-proxy/apps", nil)
	forwarded.Header.Set("Authorization", "Bearer token")
	entries := requestLines(t, router, refused, forwarded)

	require.Len(t, entries, 2, "one line for each request")
	require.Equal(t, http.StatusUnauthorized, entries[0].Status)
	require.Equal(t, "deny", entries[0].RBACDecision)
	require.Equal(t, http.StatusOK, entries[1].Status)
	require.Equal(t, "allow", entries[1].RBACDecision)
}

type contextKey string

// A request line's decision: what Authorize decided, "deny" for any refused request, and "not_evaluated" for a
// route without authorization.
func TestRequestLoggerDecisions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupTestDatabase(t)
	manager, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	viewer := viewerUser(t, database)

	router := gin.New()
	router.Use(RequestLogger(audit.NewLogger(nil), "staging", false))
	router.GET("/api/v1/auth/status", func(c *gin.Context) { c.Status(http.StatusOK) })
	authenticated := router.Group("/api/v1")
	authenticated.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization"})
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, viewer))
	})
	authenticated.Use(Authorize(manager, nil))
	authenticated.GET("/settings", func(c *gin.Context) { c.Status(http.StatusOK) })
	authenticated.POST("/users", func(c *gin.Context) { c.Status(http.StatusCreated) })

	signedIn := func(method, path string) *http.Request {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer token")
		return req
	}
	entries := requestLines(t, router,
		httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil),
		signedIn(http.MethodGet, "/api/v1/settings"),
		signedIn(http.MethodPost, "/api/v1/users"),
	)

	decisions := make([]string, 0, len(entries))
	for _, entry := range entries {
		decisions = append(decisions, entry.RBACDecision)
	}
	require.Equal(t, []string{"not_evaluated", "deny", "allow", "deny"}, decisions)
}

// A decision header Authorize set is kept unless the request was refused after it; a client can't set one (see
// StripInternalHeaders).
func TestRequestDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		status   int
		header   string
		decision string
	}{
		{http.StatusOK, "", "not_evaluated"},
		{http.StatusOK, "allow", "allow"},
		{http.StatusInternalServerError, "error", "error"},
		{http.StatusForbidden, "allow", "deny"},
		{http.StatusTooManyRequests, "", "deny"},
		{http.StatusUnauthorized, "", "deny"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		if tc.header != "" {
			c.Request.Header.Set("X-RBAC-Decision", tc.header)
		}
		c.Status(tc.status)
		c.Writer.WriteHeaderNow()
		require.Equal(t, tc.decision, requestDecision(c), "%q %d", tc.header, tc.status)
	}
}
