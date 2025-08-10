package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
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
		},
		userRoles: map[string][]string{
			"admin@example.com":  {"admin"},
			"viewer@example.com": {"viewer"},
			"ops@example.com":    {"ops"},
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
	handler := NewHandler(rbacManager, "", nil)

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
			wantUsers:  3,
		},
		{
			name:       "viewer cannot list users",
			userEmail:  "viewer@example.com",
			wantStatus: http.StatusForbidden,
			wantUsers:  0,
		},
		{
			name:       "ops cannot list users",
			userEmail:  "ops@example.com",
			wantStatus: http.StatusForbidden,
			wantUsers:  0,
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
			handler := NewHandler(rbacManager, "", nil)

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
			handler := NewHandler(rbacManager, "", nil)

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
			handler := NewHandler(rbacManager, "", nil)

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
	handler := NewHandler(rbacManager, "", nil)

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
	handler := NewHandler(rbacManager, "", nil)

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
	handler := NewHandler(rbacManager, "", nil)

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
	handler := NewHandler(rbacManager, "", nil)

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
