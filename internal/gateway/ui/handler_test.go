package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/DocSpring/convox-gateway/internal/testutil/dbtest"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock RBAC Manager for testing
type mockRBACManager struct {
	users       map[string]*rbac.UserConfig
	userRoles   map[string][]string
	shouldError bool
}

func newMockRBACManager() *mockRBACManager {
	return &mockRBACManager{
		users: map[string]*rbac.UserConfig{
			"admin@example.com": {
				Name:  "Admin User",
				Roles: []string{"admin"},
			},
			"viewer@example.com": {
				Name:  "Viewer User",
				Roles: []string{"viewer"},
			},
			"ops@example.com": {
				Name:  "Ops User",
				Roles: []string{"ops"},
			},
			"deployer@example.com": {
				Name:  "Deployer User",
				Roles: []string{"deployer"},
			},
		},
		userRoles: map[string][]string{
			"admin@example.com":    {"admin"},
			"viewer@example.com":   {"viewer"},
			"ops@example.com":      {"ops"},
			"deployer@example.com": {"deployer"},
		},
	}
}

func (m *mockRBACManager) Enforce(user, resource, action string) (bool, error) {
	if m.shouldError {
		return false, assert.AnError
	}
	// Only admins can manage users
	roles := m.userRoles[user]
	for _, role := range roles {
		if role == "admin" {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRBACManager) GetUser(email string) (*rbac.UserConfig, error) {
	if m.shouldError {
		return nil, assert.AnError
	}
	user, ok := m.users[email]
	if !ok {
		return nil, nil
	}
	return user, nil
}

func (m *mockRBACManager) GetUserWithID(email string) (*rbac.UserWithID, error) {
	user, err := m.GetUser(email)
	if err != nil || user == nil {
		return nil, err
	}
	return &rbac.UserWithID{
		ID:    1,
		Name:  user.Name,
		Roles: user.Roles,
	}, nil
}

func (m *mockRBACManager) GetUsers() (map[string]*rbac.UserConfig, error) {
	if m.shouldError {
		return nil, assert.AnError
	}
	return m.users, nil
}

func (m *mockRBACManager) SaveUser(email string, user *rbac.UserConfig) error {
	if m.shouldError {
		return assert.AnError
	}
	m.users[email] = user
	m.userRoles[email] = user.Roles
	return nil
}

func (m *mockRBACManager) DeleteUser(email string) error {
	if m.shouldError {
		return assert.AnError
	}
	delete(m.users, email)
	delete(m.userRoles, email)
	return nil
}

func (m *mockRBACManager) GetUserRoles(email string) ([]string, error) {
	if m.shouldError {
		return nil, assert.AnError
	}
	return m.userRoles[email], nil
}

func (m *mockRBACManager) GetAllowedDomain() string {
	return "example.com"
}

func (m *mockRBACManager) GetRolePermissions(role string) ([]string, error) {
	// Not used in these tests
	return []string{}, nil
}

// Helper function to create a request with auth context
func createAuthenticatedRequest(method, path string, body interface{}, email string) *http.Request {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}

	req := httptest.NewRequest(method, path, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Add auth context
	ctx := context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: email,
		Name:  "Test User",
	})

	return req.WithContext(ctx)
}

func TestListUsers(t *testing.T) {
	rbacManager := newMockRBACManager()
	handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	tests := []struct {
		name       string
		userEmail  string
		wantStatus int
		wantUsers  int
	}{
		{
			name:       "admin can list users",
			userEmail:  "admin@example.com",
			wantStatus: http.StatusOK,
			wantUsers:  4,
		},
		{
			name:       "viewer can list users",
			userEmail:  "viewer@example.com",
			wantStatus: http.StatusOK,
			wantUsers:  4,
		},
		{
			name:       "ops can list users",
			userEmail:  "ops@example.com",
			wantStatus: http.StatusOK,
			wantUsers:  4,
		},
		{
			name:       "unknown user cannot list users",
			userEmail:  "unknown@example.com",
			wantStatus: http.StatusForbidden,
			wantUsers:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createAuthenticatedRequest("GET", "/.gateway/admin/users", nil, tt.userEmail)
			rr := httptest.NewRecorder()

			handler.ListUsers(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusOK {
				var users []map[string]interface{}
				err := json.Unmarshal(rr.Body.Bytes(), &users)
				require.NoError(t, err)
				assert.Len(t, users, tt.wantUsers)
			}
		})
	}
}

func TestCreateUser(t *testing.T) {
	tests := []struct {
		name       string
		userEmail  string
		reqBody    interface{}
		wantStatus int
	}{
		{
			name:      "admin invalid email format",
			userEmail: "admin@example.com",
			reqBody: map[string]interface{}{
				"email": "not-an-email",
				"name":  "Bad",
				"roles": []string{"viewer"},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:      "admin can create user",
			userEmail: "admin@example.com",
			reqBody: map[string]interface{}{
				"email": "newuser@example.com",
				"name":  "New User",
				"roles": []string{"viewer"},
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:      "viewer cannot create user",
			userEmail: "viewer@example.com",
			reqBody: map[string]interface{}{
				"email": "newuser@example.com",
				"name":  "New User",
				"roles": []string{"viewer"},
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "ops cannot create user",
			userEmail: "ops@example.com",
			reqBody: map[string]interface{}{
				"email": "newuser@example.com",
				"name":  "New User",
				"roles": []string{"viewer"},
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "admin cannot create duplicate user",
			userEmail: "admin@example.com",
			reqBody: map[string]interface{}{
				"email": "admin@example.com", // Already exists
				"name":  "Duplicate User",
				"roles": []string{"viewer"},
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "admin with invalid request body",
			userEmail:  "admin@example.com",
			reqBody:    "invalid json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rbacManager := newMockRBACManager()
			handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

			req := createAuthenticatedRequest("POST", "/.gateway/admin/users", tt.reqBody, tt.userEmail)
			rr := httptest.NewRecorder()

			handler.CreateUser(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestDeleteUser(t *testing.T) {
	tests := []struct {
		name        string
		userEmail   string
		deleteEmail string
		wantStatus  int
	}{
		{
			name:        "admin can delete user",
			userEmail:   "admin@example.com",
			deleteEmail: "viewer@example.com",
			wantStatus:  http.StatusNoContent,
		},
		{
			name:        "viewer cannot delete user",
			userEmail:   "viewer@example.com",
			deleteEmail: "ops@example.com",
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "ops cannot delete user",
			userEmail:   "ops@example.com",
			deleteEmail: "viewer@example.com",
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "admin deleting non-existent user succeeds",
			userEmail:   "admin@example.com",
			deleteEmail: "nonexistent@example.com",
			wantStatus:  http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rbacManager := newMockRBACManager()
			handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

			req := createAuthenticatedRequest("DELETE", "/.gateway/admin/users/"+tt.deleteEmail, nil, tt.userEmail)

			// Add chi URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("email", tt.deleteEmail)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()

			handler.DeleteUser(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			// Verify user was actually deleted if successful
			if tt.wantStatus == http.StatusNoContent && tt.deleteEmail != "nonexistent@example.com" {
				user, _ := rbacManager.GetUser(tt.deleteEmail)
				assert.Nil(t, user, "User should have been deleted")
			}
		})
	}
}

func TestUpdateUserRoles(t *testing.T) {
	tests := []struct {
		name        string
		userEmail   string
		targetEmail string
		reqBody     interface{}
		wantStatus  int
	}{
		{
			name:        "admin can update user roles",
			userEmail:   "admin@example.com",
			targetEmail: "viewer@example.com",
			reqBody: map[string]interface{}{
				"roles": []string{"ops", "deployer"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "viewer cannot update user roles",
			userEmail:   "viewer@example.com",
			targetEmail: "ops@example.com",
			reqBody: map[string]interface{}{
				"roles": []string{"admin"},
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:        "ops cannot update user roles",
			userEmail:   "ops@example.com",
			targetEmail: "viewer@example.com",
			reqBody: map[string]interface{}{
				"roles": []string{"admin"},
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:        "admin updating non-existent user",
			userEmail:   "admin@example.com",
			targetEmail: "nonexistent@example.com",
			reqBody: map[string]interface{}{
				"roles": []string{"viewer"},
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:        "admin with invalid request body",
			userEmail:   "admin@example.com",
			targetEmail: "viewer@example.com",
			reqBody:     "invalid json",
			wantStatus:  http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rbacManager := newMockRBACManager()
			handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

			req := createAuthenticatedRequest("PUT", "/.gateway/admin/users/"+tt.targetEmail+"/roles", tt.reqBody, tt.userEmail)

			// Add chi URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("email", tt.targetEmail)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()

			handler.UpdateUserRoles(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			// Verify roles were actually updated if successful
			if tt.wantStatus == http.StatusOK {
				user, _ := rbacManager.GetUser(tt.targetEmail)
				require.NotNil(t, user)
				expectedRoles := tt.reqBody.(map[string]interface{})["roles"].([]string)
				assert.Equal(t, expectedRoles, user.Roles)
			}
		})
	}
}

func TestIsAdminHelper(t *testing.T) {
	rbacManager := newMockRBACManager()
	handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	tests := []struct {
		name      string
		userEmail string
		wantAdmin bool
	}{
		{
			name:      "admin user is admin",
			userEmail: "admin@example.com",
			wantAdmin: true,
		},
		{
			name:      "viewer user is not admin",
			userEmail: "viewer@example.com",
			wantAdmin: false,
		},
		{
			name:      "ops user is not admin",
			userEmail: "ops@example.com",
			wantAdmin: false,
		},
		{
			name:      "unknown user is not admin",
			userEmail: "unknown@example.com",
			wantAdmin: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createAuthenticatedRequest("GET", "/test", nil, tt.userEmail)
			result := handler.isAdmin(req)
			assert.Equal(t, tt.wantAdmin, result)
		})
	}
}

func TestNoAuthContext(t *testing.T) {
	rbacManager := newMockRBACManager()
	handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	// Test requests without auth context
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name:    "ListUsers without auth",
			handler: handler.ListUsers,
		},
		{
			name:    "CreateUser without auth",
			handler: handler.CreateUser,
		},
		{
			name:    "DeleteUser without auth",
			handler: handler.DeleteUser,
		},
		{
			name:    "UpdateUserRoles without auth",
			handler: handler.UpdateUserRoles,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()

			tt.handler(rr, req)

			assert.Equal(t, http.StatusForbidden, rr.Code, "Should return forbidden without auth")
		})
	}
}

func TestRBACManagerError(t *testing.T) {
	rbacManager := newMockRBACManager()
	rbacManager.shouldError = true
	handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	t.Run("ListUsers with RBAC error", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/.gateway/admin/users", nil, "admin@example.com")
		rr := httptest.NewRecorder()

		handler.ListUsers(rr, req)

		// When GetUserRoles fails, isAdmin returns false, so we get forbidden
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("CreateUser with RBAC error", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"email": "newuser@example.com",
			"name":  "New User",
			"roles": []string{"viewer"},
		}
		req := createAuthenticatedRequest("POST", "/.gateway/admin/users", reqBody, "admin@example.com")
		rr := httptest.NewRecorder()

		handler.CreateUser(rr, req)

		// When GetUserRoles fails, isAdmin returns false, so we get forbidden
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

// Test concurrent access to ensure thread safety
func TestConcurrentUserOperations(t *testing.T) {
	rbacManager := newMockRBACManager()
	handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	// Run multiple operations concurrently
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			req := createAuthenticatedRequest("GET", "/.gateway/admin/users", nil, "admin@example.com")
			rr := httptest.NewRecorder()
			handler.ListUsers(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func createTempDB(t *testing.T) *db.Database {
	t.Helper()
	return dbtest.NewDatabase(t)
}

func TestListAuditLogs_EmptyAndFiltered(t *testing.T) {
	rbacManager := newMockRBACManager()
	database := createTempDB(t)
	handler := NewHandler(rbacManager, "", nil, database, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	// No logs yet
	req := createAuthenticatedRequest("GET", "/.gateway/admin/audit?range=7d", nil, "admin@example.com")
	rr := httptest.NewRecorder()
	handler.ListAuditLogs(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	var logs []map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &logs))
	assert.Len(t, logs, 0)

	// Seed some logs
	now := time.Now().UTC()
	require.NoError(t, database.CreateAuditLog(&db.AuditLog{UserEmail: "admin@example.com", ActionType: "users", Action: "user.create", Status: "success", ResponseTimeMs: 10}))
	require.NoError(t, database.CreateAuditLog(&db.AuditLog{UserEmail: "viewer@example.com", ActionType: "convox", Action: "apps.list", Status: "success", ResponseTimeMs: 5}))

	// Query all
	rr = httptest.NewRecorder()
	handler.ListAuditLogs(rr, createAuthenticatedRequest("GET", "/.gateway/admin/audit?range=all", nil, "admin@example.com"))
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &logs))
	assert.GreaterOrEqual(t, len(logs), 2)

	// Filter by status
	rr = httptest.NewRecorder()
	handler.ListAuditLogs(rr, createAuthenticatedRequest("GET", "/.gateway/admin/audit?status=success", nil, "admin@example.com"))
	require.Equal(t, http.StatusOK, rr.Code)

	// Filter by recent range (1h)
	_ = now // explicitly unused here; rely on timestamps defaulting to now
	rr = httptest.NewRecorder()
	handler.ListAuditLogs(rr, createAuthenticatedRequest("GET", "/.gateway/admin/audit?range=1h", nil, "admin@example.com"))
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestListAuditLogs_Range15mFilters(t *testing.T) {
	rbacManager := newMockRBACManager()
	database := createTempDB(t)
	handler := NewHandler(rbacManager, "", nil, database, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	// Seed: one old (1h ago), one recent (5m ago)
	_ = time.Now().UTC()

	// Insert explicitly with custom timestamps
	_, err := database.DB().Exec(
		`INSERT INTO audit_logs (timestamp, user_email, user_name, action_type, action, resource, details, ip_address, user_agent, status, response_time_ms)
         VALUES (NOW() - interval '1 hour', 'old@example.com', '', 'auth', 'login', '', '', '', '', 'success', 10)`)
	require.NoError(t, err)
	_, err = database.DB().Exec(
		`INSERT INTO audit_logs (timestamp, user_email, user_name, action_type, action, resource, details, ip_address, user_agent, status, response_time_ms)
         VALUES (NOW() - interval '5 minutes', 'recent@example.com', '', 'auth', 'login', '', '', '', '', 'success', 10)`)
	require.NoError(t, err)

	// Query last 15 minutes
	rr := httptest.NewRecorder()
	req := createAuthenticatedRequest("GET", "/.gateway/admin/audit?range=15m", nil, "admin@example.com")
	handler.ListAuditLogs(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var logs []db.AuditLog
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &logs))

	// Expect only the recent entry
	require.GreaterOrEqual(t, len(logs), 1)
	for _, l := range logs {
		if l.UserEmail == "old@example.com" {
			t.Fatalf("unexpected old log included for 15m range: %+v", l)
		}
	}
}

func TestExportAuditLogs_CSV(t *testing.T) {
	rbacManager := newMockRBACManager()
	database := createTempDB(t)
	handler := NewHandler(rbacManager, "", nil, database, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	// Seed
	require.NoError(t, database.CreateAuditLog(&db.AuditLog{UserEmail: "admin@example.com", ActionType: "auth", Action: "login", Status: "success", ResponseTimeMs: 1}))

	rr := httptest.NewRecorder()
	handler.ExportAuditLogs(rr, createAuthenticatedRequest("GET", "/.gateway/admin/audit/export?range=all", nil, "admin@example.com"))
	assert.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "timestamp,user_email,user_name,action_type,action,command,resource,status,response_time_ms,ip_address,user_agent")
	assert.Contains(t, body, "admin@example.com")
}

func TestServeStaticRedirectsRack(t *testing.T) {
	rbacManager := newMockRBACManager()
	handler := NewHandler(rbacManager, "", nil, nil, nil, "test", config.RackConfig{}, "", "http://localhost:8447")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.gateway/web/", nil)
	handler.ServeStatic(rr, req)

	assert.Equal(t, http.StatusTemporaryRedirect, rr.Code)
	assert.Equal(t, "/.gateway/web/rack", rr.Header().Get("Location"))
}

// Mock email sender to capture notifications
type mockEmailSender struct {
	sent      []struct{ To, Subject, Text, HTML string }
	sentBatch []struct {
		To                  []string
		Subject, Text, HTML string
	}
}

func (m *mockEmailSender) Send(to, subject, textBody, htmlBody string) error {
	m.sent = append(m.sent, struct{ To, Subject, Text, HTML string }{to, subject, textBody, htmlBody})
	return nil
}
func (m *mockEmailSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	cp := make([]string, len(to))
	copy(cp, to)
	m.sentBatch = append(m.sentBatch, struct {
		To                  []string
		Subject, Text, HTML string
	}{cp, subject, textBody, htmlBody})
	return nil
}

func TestCreateUser_SendsEmails(t *testing.T) {
	rbacManager := newMockRBACManager()
	mailer := &mockEmailSender{}
	handler := NewHandler(rbacManager, "", nil, nil, mailer, "testrack", config.RackConfig{}, "", "http://localhost:8447")

	reqBody := map[string]interface{}{
		"email": "newuser@example.com",
		"name":  "New User",
		"roles": []string{"viewer"},
	}
	req := createAuthenticatedRequest("POST", "/.gateway/admin/users", reqBody, "admin@example.com")
	rr := httptest.NewRecorder()
	handler.CreateUser(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)
	// One direct email to user
	require.GreaterOrEqual(t, len(mailer.sent), 1)
	assert.Equal(t, "newuser@example.com", mailer.sent[0].To)
	assert.Contains(t, mailer.sent[0].Subject, "testrack")
	// One batch email to admins, includes admin@example.com
	require.GreaterOrEqual(t, len(mailer.sentBatch), 1)
	foundAdmin := false
	for _, addr := range mailer.sentBatch[0].To {
		if addr == "admin@example.com" {
			foundAdmin = true
			break
		}
	}
	assert.True(t, foundAdmin, "admin@example.com should receive admin notification")
}

func TestCreateAPIToken_SendsEmails(t *testing.T) {
	rbacManager := newMockRBACManager()
	database := createTempDB(t)
	tokenService := token.NewService(database)
	mailer := &mockEmailSender{}
	handler := NewHandler(rbacManager, "", tokenService, database, mailer, "testrack", config.RackConfig{}, "", "http://localhost:8447")

	// Seed DB user for foreign key (viewer will receive the token)
	_, err := database.CreateUser("viewer@example.com", "Viewer User", []string{"viewer"})
	require.NoError(t, err)

	// Admin creates a token for viewer
	reqBody := map[string]interface{}{
		"name":       "ci-token",
		"user_email": "viewer@example.com",
	}
	req := createAuthenticatedRequest("POST", "/.gateway/admin/tokens", reqBody, "admin@example.com")
	rr := httptest.NewRecorder()
	handler.CreateAPIToken(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.GreaterOrEqual(t, len(mailer.sent), 1)
	assert.Equal(t, "viewer@example.com", mailer.sent[0].To)
	assert.Contains(t, mailer.sent[0].Subject, "testrack")
	// Admin notification batch should include admin@example.com
	require.GreaterOrEqual(t, len(mailer.sentBatch), 1)
	hasAdmin := false
	for _, addr := range mailer.sentBatch[0].To {
		if addr == "admin@example.com" {
			hasAdmin = true
		}
	}
	assert.True(t, hasAdmin)

	// Clear and test self-created token (deployer creates own)
	mailer.sent = nil
	mailer.sentBatch = nil
	reqBody2 := map[string]interface{}{
		"name": "dev-token",
	}
	// Seed DB for deployer user as token owner
	_, err = database.CreateUser("deployer@example.com", "Deployer User", []string{"deployer"})
	require.NoError(t, err)
	req2 := createAuthenticatedRequest("POST", "/.gateway/admin/tokens", reqBody2, "deployer@example.com")
	rr2 := httptest.NewRecorder()
	handler.CreateAPIToken(rr2, req2)
	require.Equal(t, http.StatusOK, rr2.Code)
	require.GreaterOrEqual(t, len(mailer.sent), 1)
	assert.Equal(t, "deployer@example.com", mailer.sent[0].To)
	require.GreaterOrEqual(t, len(mailer.sentBatch), 1)
}
