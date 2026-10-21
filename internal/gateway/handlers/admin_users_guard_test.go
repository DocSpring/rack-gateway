package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func seedAdminAndUser(t *testing.T, rbacManager rbac.Manager) {
	t.Helper()
	for email, role := range map[string]string{"admin@example.com": "admin", "user@example.com": "viewer"} {
		require.NoError(t, rbacManager.CreateUser(email, &rbac.UserConfig{Name: email, Roles: []string{role}}))
	}
}

func putUser(handler *handlers.AdminHandler, email string, payload map[string]any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	c, w := newGinContext(http.MethodPut, "/api/v1/users/"+email, body)
	c.Params = gin.Params{{Key: "email", Value: email}}
	attachAdminContext(c)
	handler.UpdateUser(c)
	return w
}

func requireUser(t *testing.T, database *db.Database, email string, roles ...string) {
	t.Helper()
	user, err := database.GetUser(email)
	require.NoError(t, err)
	require.NotNil(t, user, email)
	require.Equal(t, roles, user.Roles)
}

// Creating a user never changes an existing one, so it can't lower an admin's roles (the caller's own included)
// past the update path's checks, or record a role change as a creation.
func TestCreateUserRefusesAnExistingEmail(t *testing.T) {
	handler, database, rbacManager := newAdminHandler(t)
	seedAdminAndUser(t, rbacManager)

	body, _ := json.Marshal(map[string]any{"email": "admin@example.com", "name": "Admin", "roles": []string{"viewer"}})
	c, w := newGinContext(http.MethodPost, "/api/v1/users", body)
	attachAdminContext(c)
	handler.CreateUser(c)

	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireUser(t, database, "admin@example.com", "admin")
}

// Only an address in the allowed domain can sign in, so no other can be a user's email.
func TestUserEmailsMustBeInTheAllowedDomain(t *testing.T) {
	handler, database, rbacManager := newAdminHandler(t)
	seedAdminAndUser(t, rbacManager)

	body, _ := json.Marshal(map[string]any{"email": "a@other.com", "name": "Someone", "roles": []string{"viewer"}})
	c, w := newGinContext(http.MethodPost, "/api/v1/users", body)
	attachAdminContext(c)
	handler.CreateUser(c)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w = putUser(handler, "user@example.com", map[string]any{"email": "user@other.com"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireUser(t, database, "user@example.com", "viewer")

	w = putUser(handler, "user@example.com", map[string]any{"email": " New.User@Example.com "})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	requireUser(t, database, "new.user@example.com", "viewer")
}

// Every field is checked before anything is written: a rejected request never changes the email (and so never
// revokes the user's sessions).
func TestUpdateUserChecksEveryFieldBeforeWriting(t *testing.T) {
	handler, database, rbacManager := newAdminHandler(t)
	seedAdminAndUser(t, rbacManager)

	for _, roles := range [][]string{{}, {"superuser"}} {
		w := putUser(handler, "user@example.com", map[string]any{"email": "moved@example.com", "roles": roles})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		requireUser(t, database, "user@example.com", "viewer")
	}
}

// The self-role guard compares user IDs, so a new email in the same request doesn't get past it. Sending your own
// roles back unchanged (as the web UI does after an email change) is fine.
func TestUpdateUserGuardsTheCallersOwnRoles(t *testing.T) {
	handler, database, rbacManager := newAdminHandler(t)
	seedAdminAndUser(t, rbacManager)

	w := putUser(handler, "admin@example.com", map[string]any{"email": "boss@example.com", "roles": []string{"viewer"}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "cannot change your own roles")
	requireUser(t, database, "admin@example.com", "admin")

	w = putUser(handler, "admin@example.com", map[string]any{"name": "Boss", "roles": []string{"admin"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	requireUser(t, database, "admin@example.com", "admin")
}
