package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type denyAllRBAC struct{}

func newDenyAllRBAC() *denyAllRBAC {
	return &denyAllRBAC{}
}

func (d *denyAllRBAC) Enforce(
	userEmail string,
	scope rbac.Scope,
	resource rbac.Resource,
	action rbac.Action,
) (bool, error) {
	return false, nil
}

func (d *denyAllRBAC) EnforceUser(
	user *db.User,
	scope rbac.Scope,
	resource rbac.Resource,
	action rbac.Action,
) (bool, error) {
	return false, nil
}

func (d *denyAllRBAC) EnforceForAPIToken(
	tokenID int64,
	scope rbac.Scope,
	resource rbac.Resource,
	action rbac.Action,
) (bool, error) {
	return false, nil
}

func (d *denyAllRBAC) GetAllowedDomain() string {
	return "example.com"
}

func (d *denyAllRBAC) GetUserByEmail(email string) (*db.User, error) {
	return nil, nil
}

func (d *denyAllRBAC) DeleteUser(email string) error {
	return nil
}

func (d *denyAllRBAC) GetUser(email string) (*rbac.UserConfig, error) {
	return nil, nil
}

func (d *denyAllRBAC) GetUserWithID(email string) (*rbac.UserWithID, error) {
	return nil, nil
}

func (d *denyAllRBAC) GetUsers() (map[string]*rbac.UserConfig, error) {
	return nil, nil
}

func (d *denyAllRBAC) SaveUser(email string, user *rbac.UserConfig) error {
	return nil
}

func (d *denyAllRBAC) GetUserRoles(email string) ([]string, error) {
	return nil, nil
}

func (d *denyAllRBAC) GetRolePermissions(role string) ([]string, error) {
	return nil, nil
}

func TestSlackOAuthAuthorizeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	cfg := &config.Config{
		SlackClientID:     "test-client-id",
		SlackClientSecret: "test-secret",
		Domain:            "gateway.example.com",
	}

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(user),
		database: database,
		config:   cfg,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/oauth/authorize", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.SlackOAuthAuthorizeHandler(c)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Contains(t, result["authorization_url"], "https://slack.com/oauth/v2/authorize")
	require.Contains(t, result["authorization_url"], "client_id=test-client-id")
	require.Contains(t, result["authorization_url"], "scope=channels%3Aread%2Cchat%3Awrite")
}

func TestGetSlackIntegration_NotFound(t *testing.T) {
	env := setupSlackTestEnv(t)
	resp := env.callGetSlackIntegration()
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetSlackIntegration_Found(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create Slack integration
	channelActions := map[string]interface{}{
		"security": map[string]interface{}{
			"id":      "C123456",
			"name":    "#security",
			"actions": []string{"mfa.*", "auth.*"},
		},
	}
	botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
	integration, err := database.CreateSlackIntegration(
		"T123456",
		"Test Workspace",
		botToken,
		"U123456",
		"channels:read,chat:write",
		channelActions,
		&user.ID,
	)
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(user),
		database: database,
		config:   &config.Config{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.GetSlackIntegrationHandler(c)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result db.SlackIntegration
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, integration.WorkspaceID, result.WorkspaceID)
	require.Equal(t, integration.WorkspaceName, result.WorkspaceName)
	require.NotEmpty(t, result.ChannelActions)
}

func TestUpdateSlackChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create Slack integration
	channelActions := map[string]interface{}{
		"security": map[string]interface{}{
			"id":      "C123456",
			"name":    "#security",
			"actions": []string{"mfa.*"},
		},
	}
	botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
	_, err = database.CreateSlackIntegration(
		"T123456",
		"Test Workspace",
		botToken,
		"U123456",
		"channels:read,chat:write",
		channelActions,
		&user.ID,
	)
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(user),
		database: database,
		config:   &config.Config{},
	}

	// Update channel actions
	updatedActions := map[string]interface{}{
		"security": map[string]interface{}{
			"id":      "C123456",
			"name":    "#security",
			"actions": []string{"mfa.*", "auth.*", "api-token.*"},
		},
		"infrastructure": map[string]interface{}{
			"id":      "C789012",
			"name":    "#infrastructure",
			"actions": []string{"deploy-approval-request.*"},
		},
	}

	payload, err := json.Marshal(map[string]interface{}{
		"channel_actions": updatedActions,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/integrations/slack/channels",
		strings.NewReader(string(payload)),
	)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.UpdateSlackChannelsHandler(c)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify update
	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.NotNil(t, integration)

	securityChannel := integration.ChannelActions["security"].(map[string]interface{})
	require.Equal(t, "C123456", securityChannel["id"])
	actions := securityChannel["actions"].([]interface{})
	require.Len(t, actions, 3)

	infraChannel := integration.ChannelActions["infrastructure"].(map[string]interface{})
	require.Equal(t, "C789012", infraChannel["id"])
}

func TestDeleteSlackIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create Slack integration
	channelActions := map[string]interface{}{
		"security": map[string]interface{}{
			"id":      "C123456",
			"name":    "#security",
			"actions": []string{"mfa.*"},
		},
	}
	botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
	_, err = database.CreateSlackIntegration(
		"T123456",
		"Test Workspace",
		botToken,
		"U123456",
		"channels:read,chat:write",
		channelActions,
		&user.ID,
	)
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(user),
		database: database,
		config:   &config.Config{},
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/slack", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.DeleteSlackIntegrationHandler(c)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify deletion
	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.Nil(t, integration)
}

func TestUpdateSlackChannels_NoIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	auditLogger := audit.NewLogger(database)
	handler := &AdminHandler{
		rbac:        newAllowAllRBAC(user),
		database:    database,
		config:      &config.Config{},
		auditLogger: auditLogger,
	}

	payload, err := json.Marshal(map[string]interface{}{
		"channel_actions": map[string]interface{}{},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/integrations/slack/channels",
		strings.NewReader(string(payload)),
	)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.UpdateSlackChannelsHandler(c)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestEnforceIntegrationPermission_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	handler := &AdminHandler{
		database: database,
		config:   &config.Config{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	allowed := handler.enforceIntegrationPermission(c, rbac.ActionRead)
	require.False(t, allowed)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	var result map[string]string
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "authentication required", result["error"])
}

func TestEnforceIntegrationPermission_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("user@example.com", "Regular User", []string{"developer"})
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newDenyAllRBAC(),
		database: database,
		config:   &config.Config{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	allowed := handler.enforceIntegrationPermission(c, rbac.ActionRead)
	require.False(t, allowed)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "insufficient permissions", result["error"])
}

func TestLoadSlackIntegration_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	handler := &AdminHandler{
		database: database,
		config:   &config.Config{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	integration := handler.loadSlackIntegration(c)
	require.Nil(t, integration)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	var result map[string]string
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "no Slack integration found", result["error"])
}

func TestCreateSlackClient_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	handler := &AdminHandler{
		database: database,
		config:   &config.Config{},
	}

	// Create integration with invalid base64 token
	integration := &db.SlackIntegration{
		ID:                1,
		WorkspaceID:       "T123456",
		WorkspaceName:     "Test Workspace",
		BotTokenEncrypted: "!invalid-base64!",
		BotUserID:         "U123456",
		Scope:             "channels:read,chat:write",
		ChannelActions:    map[string]interface{}{},
		CreatedByUserID:   &user.ID,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/test", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	client := handler.createSlackClient(c, integration)
	require.Nil(t, client)

	resp := w.Result()
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "failed to decrypt token", result["error"])
}

type slackTestEnv struct {
	t        *testing.T
	database *db.Database
	handler  *AdminHandler
	user     *db.User
}

func setupSlackTestEnv(t *testing.T) *slackTestEnv {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newAllowAllRBAC(user),
		database: database,
		config:   &config.Config{},
	}

	return &slackTestEnv{
		t:        t,
		database: database,
		handler:  handler,
		user:     user,
	}
}

func (e *slackTestEnv) callGetSlackIntegration() *http.Response {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email: e.user.Email,
		Name:  e.user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", e.user.Email)

	e.handler.GetSlackIntegrationHandler(c)
	return w.Result()
}
