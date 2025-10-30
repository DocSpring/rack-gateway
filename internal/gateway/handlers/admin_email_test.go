package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	emailpkg "github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

func newAdminHandler(t *testing.T, sender emailpkg.Sender) (*handlers.AdminHandler, *db.Database, rbac.RBACManager) {
	t.Helper()

	database := dbtest.NewDatabase(t)
	rbacManager, err := rbac.NewDBManager(database, "example.com")
	if err != nil {
		t.Fatalf("failed to create rbac manager: %v", err)
	}

	tokenService := token.NewService(database)

	cfg := &config.Config{
		Domain: "gateway.example.com",
		Racks: map[string]config.RackConfig{
			"default": {
				Name:    "staging",
				Alias:   "Staging",
				Enabled: true,
			},
		},
	}

	// Get MFA settings from settings service
	settingsService := settings.NewService(database)
	mfaSettings, _ := settingsService.GetMFASettings()
	auditLogger := audit.NewLogger(database)
	handler := handlers.NewAdminHandler(rbacManager, database, tokenService, sender, cfg, nil, nil, mfaSettings, auditLogger, nil, nil)
	return handler, database, rbacManager
}

func newGinContext(method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func attachUserContext(c *gin.Context, email, name string) {
	c.Set("user_email", email)
	c.Set("user_name", name)
	ctx := context.WithValue(c.Request.Context(), auth.UserContextKey, &auth.AuthUser{Email: email, Name: name})
	c.Request = c.Request.WithContext(ctx)
}

// TestCreateUserSendsWelcomeAndAdminEmails - Removed after River migration
// Email notifications are now sent asynchronously via River job queue
// Testing this requires River worker setup which is beyond the scope of unit tests

// TestCreateAPITokenSendsOwnerAndAdminEmails - Removed after River migration
// Email notifications are now sent asynchronously via River job queue
// Testing this requires River worker setup which is beyond the scope of unit tests

// TestSettingsUpdatesNotifyAdmins - Removed during settings refactor
// Settings are now managed via generic settings service and API endpoints

func TestUpdateUser_AllowsEmailNameAndRoleChanges(t *testing.T) {
	handler, database, rbacManager := newAdminHandler(t, nil)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}
	if err := rbacManager.SaveUser("user@example.com", &rbac.UserConfig{Name: "Original User", Roles: []string{"viewer"}}); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	payload := map[string]any{
		"email": "renamed@example.com",
		"name":  "Renamed User",
		"roles": []string{"deployer"},
	}
	body, _ := json.Marshal(payload)
	c, w := newGinContext(http.MethodPut, "/api/v1/users/user@example.com", body)
	c.Params = gin.Params{{Key: "email", Value: "user@example.com"}}
	attachUserContext(c, "admin@example.com", "Admin User")

	handler.UpdateUser(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summary handlers.UserSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if summary.Email != "renamed@example.com" {
		t.Fatalf("expected email to update, got %s", summary.Email)
	}
	if summary.Name != "Renamed User" {
		t.Fatalf("expected name to update, got %s", summary.Name)
	}
	if len(summary.Roles) != 1 || summary.Roles[0] != "deployer" {
		t.Fatalf("expected roles to update, got %v", summary.Roles)
	}

	dbUser, err := database.GetUser("renamed@example.com")
	if err != nil || dbUser == nil {
		t.Fatalf("expected database user at new email, err=%v user=%v", err, dbUser)
	}
	if dbUser.Name != "Renamed User" {
		t.Fatalf("expected db name to update, got %s", dbUser.Name)
	}
	if len(dbUser.Roles) != 1 || dbUser.Roles[0] != "deployer" {
		t.Fatalf("expected db roles to update, got %v", dbUser.Roles)
	}
	if existing, _ := database.GetUser("user@example.com"); existing != nil {
		t.Fatalf("expected old email entry to be removed")
	}

	rbacUser, err := rbacManager.GetUser("renamed@example.com")
	if err != nil || rbacUser == nil {
		t.Fatalf("expected rbac user at new email, err=%v user=%v", err, rbacUser)
	}
	if rbacUser.Name != "Renamed User" {
		t.Fatalf("expected rbac name to update, got %s", rbacUser.Name)
	}
	if len(rbacUser.Roles) != 1 || rbacUser.Roles[0] != "deployer" {
		t.Fatalf("expected rbac roles to update, got %v", rbacUser.Roles)
	}
}

func TestUpdateUserName_OnlyUpdatesName(t *testing.T) {
	handler, database, rbacManager := newAdminHandler(t, nil)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}
	if err := rbacManager.SaveUser("user@example.com", &rbac.UserConfig{Name: "Original User", Roles: []string{"viewer"}}); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	payload := map[string]any{
		"name": "Updated Display Name",
	}
	body, _ := json.Marshal(payload)
	c, w := newGinContext(http.MethodPut, "/api/v1/users/user@example.com/name", body)
	c.Params = gin.Params{{Key: "email", Value: "user@example.com"}}
	attachUserContext(c, "admin@example.com", "Admin User")

	handler.UpdateUserName(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summary handlers.UserSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if summary.Email != "user@example.com" {
		t.Fatalf("expected email unchanged, got %s", summary.Email)
	}
	if summary.Name != "Updated Display Name" {
		t.Fatalf("expected name to update, got %s", summary.Name)
	}
	if len(summary.Roles) != 1 || summary.Roles[0] != "viewer" {
		t.Fatalf("expected roles unchanged, got %v", summary.Roles)
	}

	dbUser, err := database.GetUser("user@example.com")
	if err != nil || dbUser == nil {
		t.Fatalf("expected database user, err=%v user=%v", err, dbUser)
	}
	if dbUser.Name != "Updated Display Name" {
		t.Fatalf("expected db name to update, got %s", dbUser.Name)
	}
	if len(dbUser.Roles) != 1 || dbUser.Roles[0] != "viewer" {
		t.Fatalf("expected db roles unchanged, got %v", dbUser.Roles)
	}
}

func TestUpdateUser_PreventsSelfRoleChange(t *testing.T) {
	handler, _, rbacManager := newAdminHandler(t, nil)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}

	payload := map[string]any{
		"roles": []string{"viewer"},
	}
	body, _ := json.Marshal(payload)
	c, w := newGinContext(http.MethodPut, "/api/v1/users/admin@example.com", body)
	c.Params = gin.Params{{Key: "email", Value: "admin@example.com"}}
	attachUserContext(c, "admin@example.com", "Admin User")

	handler.UpdateUser(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when attempting to change own roles, got %d", w.Code)
	}
}
