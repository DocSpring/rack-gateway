package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestAuthenticatedSetsRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	database := setupTestDatabase(t)
	if _, err := database.CreateUser("user@example.com", "User", []string{"viewer"}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	mgr, err := rbac.NewDBManager(database, "example.com")
	if err != nil {
		t.Fatalf("new rbac manager: %v", err)
	}

	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	service := auth.NewAuthService(nil, database, sessionManager)

	// Get user and create session
	user, err := database.GetUser("user@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	token, _, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	router := gin.New()
	router.Use(Authenticated(service, mgr))
	var sawHandler bool
	router.GET("/me", func(c *gin.Context) {
		sawHandler = true
		user, ok := auth.GetAuthUser(c.Request.Context())
		if !ok {
			c.String(http.StatusInternalServerError, "missing auth user")
			return
		}
		if user.Email != "user@example.com" {
			c.String(http.StatusInternalServerError, "unexpected user %s", user.Email)
			return
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if !sawHandler {
		t.Fatalf("handler was not executed")
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}

func setupTestDatabase(t *testing.T) *db.Database {
	t.Helper()
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	return database
}
