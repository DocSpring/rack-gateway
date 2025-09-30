package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type allowAllRBAC struct {
	users map[string]*rbac.UserWithID
}

func newAllowAllRBAC(users ...*db.User) *allowAllRBAC {
	m := make(map[string]*rbac.UserWithID)
	for _, u := range users {
		if u == nil {
			continue
		}
		rolesCopy := append([]string(nil), u.Roles...)
		m[u.Email] = &rbac.UserWithID{ID: u.ID, Name: u.Name, Roles: rolesCopy}
	}
	return &allowAllRBAC{users: m}
}

func (a *allowAllRBAC) Enforce(userEmail, resource, action string) (bool, error) {
	return true, nil
}

func (a *allowAllRBAC) GetAllowedDomain() string {
	return "example.com"
}

func (a *allowAllRBAC) GetUser(email string) (*rbac.UserConfig, error) {
	user, ok := a.users[email]
	if !ok {
		return nil, nil
	}
	rolesCopy := append([]string(nil), user.Roles...)
	return &rbac.UserConfig{Name: user.Name, Roles: rolesCopy}, nil
}

func (a *allowAllRBAC) GetUserWithID(email string) (*rbac.UserWithID, error) {
	user, ok := a.users[email]
	if !ok {
		return nil, nil
	}
	clone := *user
	clone.Roles = append([]string(nil), user.Roles...)
	return &clone, nil
}

func (a *allowAllRBAC) GetUsers() (map[string]*rbac.UserConfig, error) {
	result := make(map[string]*rbac.UserConfig, len(a.users))
	for email, user := range a.users {
		rolesCopy := append([]string(nil), user.Roles...)
		result[email] = &rbac.UserConfig{Name: user.Name, Roles: rolesCopy}
	}
	return result, nil
}

func (a *allowAllRBAC) SaveUser(email string, user *rbac.UserConfig) error {
	if user == nil {
		delete(a.users, email)
		return nil
	}
	rolesCopy := append([]string(nil), user.Roles...)
	a.users[email] = &rbac.UserWithID{Name: user.Name, Roles: rolesCopy}
	return nil
}

func (a *allowAllRBAC) DeleteUser(email string) error {
	delete(a.users, email)
	return nil
}

func (a *allowAllRBAC) GetUserRoles(email string) ([]string, error) {
	user, ok := a.users[email]
	if !ok {
		return nil, nil
	}
	return append([]string(nil), user.Roles...), nil
}

func (a *allowAllRBAC) GetRolePermissions(role string) ([]string, error) {
	return []string{}, nil
}

func createAPITokenHelper(t *testing.T, database *db.Database, userID int64) *db.APIToken {
	t.Helper()
	hash := strings.Repeat("a", 64)
	perms := []string{
		"convox:build:create-with-approval",
		"convox:object:create-with-approval",
		"convox:release:create-with-approval",
		"convox:release:promote-with-approval",
	}
	token, err := database.CreateAPIToken(hash, "ci-token", userID, perms, nil, nil)
	require.NoError(t, err)
	return token
}

func TestCreateDeployApprovalRequestResolvesTargetTokenByPublicID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("deployer@example.com", "Deploy User", []string{"deployer"})
	require.NoError(t, err)

	tokenHash := strings.Repeat("a", 64)
	permissions := []string{"convox:build:create-with-approval"}
	token, err := database.CreateAPIToken(tokenHash, "ci-token", user.ID, permissions, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, token.PublicID)

	handler := &APIHandler{
		rbac:     newAllowAllRBAC(user),
		database: database,
		config: &config.Config{Racks: map[string]config.RackConfig{
			"default": {Name: "staging", Enabled: true},
		}},
	}

	body := map[string]string{
		"message":             "Deploy release",
		"rack":                "staging",
		"target_api_token_id": token.PublicID,
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/deploy-approval-requests", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email:      user.Email,
		Name:       user.Name,
		IsAPIToken: true,
		TokenID:    &token.ID,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.CreateDeployApprovalRequest(c)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got DeployApprovalRequestResponse
	err = json.NewDecoder(resp.Body).Decode(&got)
	require.NoError(t, err)
	require.Equal(t, token.PublicID, got.TargetAPITokenID)
}

func TestPreapproveDeployCreatesApprovedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	deployer, err := database.CreateUser("ci@example.com", "CI Bot", []string{"cicd"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, deployer.ID)

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(admin, deployer),
		database: database,
		config: &config.Config{
			DeployApprovalWindow: 10 * time.Minute,
			Racks: map[string]config.RackConfig{
				"default": {Name: "staging", Enabled: true},
			},
		},
	}

	body := map[string]string{
		"message":             "CI pipeline run",
		"rack":                "staging",
		"target_api_token_id": token.PublicID,
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/deploy-approval-requests/preapprove", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{Email: admin.Email, Name: admin.Name}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", admin.Email)

	handler.PreapproveDeploy(c)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp DeployApprovalRequestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, token.PublicID, resp.TargetAPITokenID)
	require.Equal(t, "approved", strings.ToLower(resp.Status))
	require.NotNil(t, resp.ApprovalExpiresAt)
}

func TestPreapproveDeployRequiresTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(admin),
		database: database,
		config: &config.Config{
			DeployApprovalWindow: 5 * time.Minute,
			Racks: map[string]config.RackConfig{
				"default": {Name: "staging", Enabled: true},
			},
		},
	}

	body := map[string]string{
		"message": "CI pipeline run",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/deploy-approval-requests/preapprove", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{Email: admin.Email, Name: admin.Name}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", admin.Email)

	handler.PreapproveDeploy(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "target_api_token_id")
}
