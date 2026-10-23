package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func resetUserSignIn(t *testing.T, h *AdminHandler, admin *db.User, email string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+email+"/sign-in/reset", nil)
	c.Request = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email: admin.Email, Name: admin.Name, Roles: admin.Roles, DBUser: admin,
	}))
	c.Params = gin.Params{{Key: "email", Value: email}}
	c.Set("user_email", admin.Email)
	h.ResetUserSignIn(c)
	return w
}

// Resetting a user's sign-in lets a new account bind to them and signs them out, keeping their API tokens.
func TestResetUserSignIn(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := database.CreateUser("dev@example.com", "Dev", []string{"deployer"})
	require.NoError(t, err)
	require.NoError(t, database.BindUserSignInAccount(user.ID, "old-account"))
	_, err = database.CreateUserSession(user, strings.Repeat("e", 64), time.Now().Add(time.Hour), "web",
		"", "", "127.0.0.1", "ua", nil, nil)
	require.NoError(t, err)
	_, err = database.CreateAPIToken(strings.Repeat("f", 64), "dev-ci", user.ID, []string{"deployer"}, nil, nil)
	require.NoError(t, err)
	h := &AdminHandler{database: database, auditLogger: audit.NewLogger(database), config: &config.Config{}}

	w := resetUserSignIn(t, h, admin, admin.Email)
	require.Equal(t, http.StatusBadRequest, w.Code, "an admin can't reset their own sign-in")

	w = resetUserSignIn(t, h, admin, user.Email)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, database.BindUserSignInAccount(user.ID, "new-account"))
	sessions, err := database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions)
	tokens, err := database.ListAPITokensByUser(user.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)

	logs, err := database.GetAuditLogs(admin.Email, time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	require.Equal(t, "user.sign_in_reset", logs[0].Action)
	require.Equal(t, audit.StatusSuccess, logs[0].Status)
}
