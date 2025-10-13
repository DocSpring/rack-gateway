package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	"github.com/gin-gonic/gin"
)

// fakeEmailSender implements email.Sender for testing.
type fakeEmailSender struct {
	sendCalls     []emailCall
	sendManyCalls []emailCall
}

type emailCall struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

func (f *fakeEmailSender) Send(to, subject, text, html string) error {
	f.sendCalls = append(f.sendCalls, emailCall{To: []string{to}, Subject: subject, Text: text, HTML: html})
	return nil
}

func (f *fakeEmailSender) SendMany(to []string, subject, text, html string) error {
	dup := append([]string(nil), to...)
	f.sendManyCalls = append(f.sendManyCalls, emailCall{To: dup, Subject: subject, Text: text, HTML: html})
	return nil
}

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
	handler := handlers.NewAdminHandler(rbacManager, database, tokenService, sender, cfg, nil, nil, mfaSettings, auditLogger, nil)
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

func TestCreateUserSendsWelcomeAndAdminEmails(t *testing.T) {
	sender := &fakeEmailSender{}
	handler, _, rbacManager := newAdminHandler(t, sender)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}

	payload := map[string]interface{}{
		"email": "new.user@example.com",
		"name":  "New User",
		"roles": []string{"viewer"},
	}
	body, _ := json.Marshal(payload)
	c, w := newGinContext(http.MethodPost, "/api/v1/users", body)
	attachUserContext(c, "admin@example.com", "Admin User")

	handler.CreateUser(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", w.Code)
	}

	if len(sender.sendCalls) != 1 {
		t.Fatalf("expected 1 direct email, got %d", len(sender.sendCalls))
	}
	if got := sender.sendCalls[0].To[0]; got != "new.user@example.com" {
		t.Fatalf("expected welcome email to new user, got %s", got)
	}
	if len(sender.sendManyCalls) != 1 {
		t.Fatalf("expected 1 admin broadcast, got %d", len(sender.sendManyCalls))
	}
	if got := sender.sendManyCalls[0].To; len(got) != 1 || got[0] != "admin@example.com" {
		t.Fatalf("expected admin email to admin@example.com, got %+v", got)
	}
}

func TestCreateAPITokenSendsOwnerAndAdminEmails(t *testing.T) {
	sender := &fakeEmailSender{}
	handler, database, rbacManager := newAdminHandler(t, sender)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}
	if err := rbacManager.SaveUser("dev@example.com", &rbac.UserConfig{Name: "Dev", Roles: []string{"viewer"}}); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	// Ensure database has consistent state for token queries
	if _, err := database.GetUser("dev@example.com"); err != nil {
		t.Fatalf("failed to fetch seeded user: %v", err)
	}

	payload := map[string]interface{}{
		"name":        "Deploy Token",
		"user_email":  "dev@example.com",
		"permissions": []string{"convox:app:list"},
	}
	body, _ := json.Marshal(payload)
	c, w := newGinContext(http.MethodPost, "/api/v1/api-tokens", body)
	attachUserContext(c, "admin@example.com", "Admin User")

	handler.CreateAPIToken(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	if len(sender.sendCalls) != 1 {
		t.Fatalf("expected token owner email, got %d direct emails", len(sender.sendCalls))
	}
	if got := sender.sendCalls[0].To[0]; got != "dev@example.com" {
		t.Fatalf("expected owner email to dev@example.com, got %s", got)
	}
	if len(sender.sendManyCalls) != 1 {
		t.Fatalf("expected admin notification, got %d broadcasts", len(sender.sendManyCalls))
	}
	if got := sender.sendManyCalls[0].To; len(got) != 1 || got[0] != "admin@example.com" {
		t.Fatalf("expected admin recipients [admin@example.com], got %+v", got)
	}
}

// TestSettingsUpdatesNotifyAdmins - Removed during settings refactor
// Settings are now managed via generic settings service and API endpoints
