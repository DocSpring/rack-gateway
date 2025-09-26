package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/envutil"
	"github.com/DocSpring/convox-gateway/internal/gateway/handlers"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/gin-gonic/gin"
)

func newAPIHandler(t *testing.T, database *db.Database, rackURL string) (*handlers.APIHandler, rbac.RBACManager) {
	t.Helper()

	rbacManager, err := rbac.NewDBManager(database, "example.com")
	if err != nil {
		t.Fatalf("failed to create RBAC manager: %v", err)
	}

	cfg := &config.Config{
		Racks: map[string]config.RackConfig{
			"default": {
				Name:     "test",
				Alias:    "test",
				URL:      rackURL,
				Username: "convox",
				APIKey:   "token",
				Enabled:  true,
			},
		},
	}

	handler := handlers.NewAPIHandler(rbacManager, database, cfg, nil, nil)
	return handler, rbacManager
}

func newJSONContext(method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func attachUser(c *gin.Context, email, name string) {
	c.Set("user_email", email)
	c.Set("user_name", name)
	ctx := context.WithValue(c.Request.Context(), auth.UserContextKey, &auth.AuthUser{Email: email, Name: name})
	c.Request = c.Request.WithContext(ctx)
}

func TestUpdateEnvValuesSuccess(t *testing.T) {
	t.Setenv("CONVOX_SECRET_ENV_VARS", "SECRET_KEY")

	var lastEnvPayload string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"env":"FOO=bar\nSECRET_KEY=shhh"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/apps/myapp/releases":
			body, _ := io.ReadAll(r.Body)
			lastEnvPayload = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"R2"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, server.URL)

	if err := rbacManager.SaveUser("deployer@example.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	payload := map[string]interface{}{
		"app": "myapp",
		"set": map[string]string{"FOO": "baz"},
	}
	body, _ := json.Marshal(payload)
	c, w := newJSONContext(http.MethodPut, "/.gateway/api/env", body)
	attachUser(c, "deployer@example.com", "Deployer User")

	handler.UpdateEnvValues(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp handlers.UpdateEnvValuesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ReleaseID != "R2" {
		t.Fatalf("expected release ID R2, got %s", resp.ReleaseID)
	}

	if got := resp.Env["FOO"]; got != "baz" {
		t.Fatalf("expected FOO=baz in response, got %s", got)
	}

	if got := resp.Env["SECRET_KEY"]; got != envutil.MaskedSecret {
		t.Fatalf("expected SECRET_KEY masked in response, got %s", got)
	}

	values, err := url.ParseQuery(lastEnvPayload)
	if err != nil {
		t.Fatalf("failed to parse env payload: %v", err)
	}
	if got := values.Get("env"); got != "FOO=baz\nSECRET_KEY=shhh" {
		t.Fatalf("unexpected env payload posted to rack: %s", got)
	}
}

func TestUpdateEnvValuesRequiresEnvSetPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			_, _ = w.Write([]byte(`{"env":"FOO=bar"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, server.URL)

	if err := rbacManager.SaveUser("viewer@example.com", &rbac.UserConfig{Name: "Viewer", Roles: []string{"viewer"}}); err != nil {
		t.Fatalf("failed to seed viewer: %v", err)
	}

	payload := map[string]interface{}{
		"app": "myapp",
		"set": map[string]string{"FOO": "baz"},
	}
	body, _ := json.Marshal(payload)
	c, w := newJSONContext(http.MethodPut, "/.gateway/api/env", body)
	attachUser(c, "viewer@example.com", "Viewer User")

	handler.UpdateEnvValues(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestUpdateEnvValuesSecretRequiresPermission(t *testing.T) {
	t.Setenv("CONVOX_SECRET_ENV_VARS", "SECRET_KEY")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"env":"SECRET_KEY=shhh"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, server.URL)

	if err := rbacManager.SaveUser("deployer@example.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}); err != nil {
		t.Fatalf("failed to seed deployer: %v", err)
	}

	payload := map[string]interface{}{
		"app": "myapp",
		"set": map[string]string{"SECRET_KEY": "updated"},
	}
	body, _ := json.Marshal(payload)
	c, w := newJSONContext(http.MethodPut, "/.gateway/api/env", body)
	attachUser(c, "deployer@example.com", "Deployer User")

	handler.UpdateEnvValues(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when modifying secret without permission, got %d", w.Code)
	}
}

func TestUpdateEnvValuesMaskedSecretWithoutExistingValueFails(t *testing.T) {
	t.Setenv("CONVOX_SECRET_ENV_VARS", "SECRET_KEY")

	var postCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"env":"FOO=bar"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/apps/myapp/releases":
			postCalled = true
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, server.URL)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}

	payload := map[string]interface{}{
		"app": "myapp",
		"set": map[string]string{"SECRET_KEY": envutil.MaskedSecret},
	}
	body, _ := json.Marshal(payload)
	c, w := newJSONContext(http.MethodPut, "/.gateway/api/env", body)
	attachUser(c, "admin@example.com", "Admin User")

	handler.UpdateEnvValues(c)

	if postCalled {
		t.Fatalf("expected no release to be created when masked secret is provided without existing value")
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when masked secret provided without existing value, got %d", w.Code)
	}

	var resp handlers.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if resp.Error == "" {
		t.Fatalf("expected error message in response")
	}
}

func TestUpdateEnvValuesProtectedKeyDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"env":"PROTECTED=1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	database := dbtest.NewDatabase(t)
	handler, rbacManager := newAPIHandler(t, database, server.URL)

	if err := rbacManager.SaveUser("admin@example.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}

	if err := database.UpsertSetting("protected_env_vars", []string{"PROTECTED"}, nil); err != nil {
		t.Fatalf("failed to seed protected env vars: %v", err)
	}

	payload := map[string]interface{}{
		"app": "myapp",
		"set": map[string]string{"PROTECTED": "2"},
	}
	body, _ := json.Marshal(payload)
	c, w := newJSONContext(http.MethodPut, "/.gateway/api/env", body)
	attachUser(c, "admin@example.com", "Admin User")

	handler.UpdateEnvValues(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when modifying protected key, got %d", w.Code)
	}
}
