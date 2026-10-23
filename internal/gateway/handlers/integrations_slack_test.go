package handlers

import (
	"context"
	"encoding/json"
	"io"
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

func (_ *denyAllRBAC) Authorize(_ rbac.Principal, _ string) (bool, error) {
	return false, nil
}

func (_ *denyAllRBAC) GetAllowedDomain() string {
	return "example.com"
}

func (_ *denyAllRBAC) GetUserByEmail(_ string) (*db.User, error) {
	return nil, nil
}

func (_ *denyAllRBAC) DeleteUser(_ string) error {
	return nil
}

func (_ *denyAllRBAC) GetUser(_ string) (*rbac.UserConfig, error) {
	return nil, nil
}

func (_ *denyAllRBAC) GetUserWithID(_ string) (*rbac.UserWithID, error) {
	return nil, nil
}

func (_ *denyAllRBAC) GetUsers() (map[string]*rbac.UserConfig, error) {
	return nil, nil
}

func (_ *denyAllRBAC) CreateUser(_ string, _ *rbac.UserConfig) error {
	return nil
}

func (_ *denyAllRBAC) GetUserRoles(_ string) ([]string, error) {
	return nil, nil
}

func (_ *denyAllRBAC) GetRolePermissions(_ string) ([]string, error) {
	return nil, nil
}

func TestSlackOAuthAuthorizeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

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
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.SlackOAuthAuthorizeHandler(c)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetSlackIntegration_Found(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

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
	botToken := fakeSlackBot
	integration, err := database.ReplaceSlackIntegration(
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
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.GetSlackIntegrationHandler(c)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotContains(t, string(body), botToken)
	require.NotContains(t, string(body), "rgwenc:")

	var result db.SlackIntegration
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)
	require.Equal(t, integration.WorkspaceID, result.WorkspaceID)
	require.Equal(t, integration.WorkspaceName, result.WorkspaceName)
	require.NotEmpty(t, result.ChannelActions)
}

func TestUpdateSlackChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

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
	botToken := fakeSlackBot
	_, err = database.ReplaceSlackIntegration(
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

	require.Equal(t, http.StatusOK, putSlackChannels(t, handler, user, channelActions, updatedActions).Code)

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
	botToken := fakeSlackBot
	_, err = database.ReplaceSlackIntegration(
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
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.DeleteSlackIntegrationHandler(c)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify deletion
	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.Nil(t, integration)
}

func TestUpdateSlackChannels_NoIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	auditLogger := audit.NewLogger(database)
	handler := &AdminHandler{
		rbac:        newAllowAllRBAC(user),
		database:    database,
		config:      &config.Config{},
		auditLogger: auditLogger,
	}

	w := putSlackChannels(t, handler, user, map[string]interface{}{}, map[string]interface{}{})
	require.Equal(t, http.StatusNotFound, w.Code)
}

// A change made to a configuration that has changed since is refused with 409, and the newer one is kept.
func TestUpdateSlackChannels_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)
	security := func(actions ...string) map[string]interface{} {
		return map[string]interface{}{"security": map[string]interface{}{
			"id": "C123456", "name": "security", "actions": actions,
		}}
	}
	_, err = database.ReplaceSlackIntegration("T123456", "Test Workspace", fakeSlackBot, "U123456",
		"channels:read,chat:write", security("login.*_failed"), &user.ID)
	require.NoError(t, err)
	handler := &AdminHandler{rbac: newAllowAllRBAC(user), database: database, config: &config.Config{}}

	added := security("login.*_failed", "mfa.verify_failed")
	require.Equal(t, http.StatusOK, putSlackChannels(t, handler, user, security("login.*_failed"), added).Code)
	w := putSlackChannels(t, handler, user, security("login.*_failed"), security())
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "slack_channels_changed")

	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	actions := integration.ChannelActions["security"].(map[string]interface{})["actions"]
	require.Equal(t, []interface{}{"login.*_failed", "mfa.verify_failed"}, actions)
}

// A change without the configuration it was made to is refused.
func TestUpdateSlackChannels_RequiresPrevious(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)
	handler := &AdminHandler{rbac: newAllowAllRBAC(user), database: database, config: &config.Config{}}

	w := putSlackChannels(t, handler, user, nil, map[string]interface{}{})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// putSlackChannels sends PUT /integrations/slack/channels as user, for the current integration (ID 1 when there's
// none). A nil previous is left out of the request.
func putSlackChannels(
	t *testing.T,
	handler *AdminHandler,
	user *db.User,
	previous, next map[string]interface{},
) *httptest.ResponseRecorder {
	t.Helper()
	integrationID := int64(1)
	current, err := handler.database.GetSlackIntegration()
	require.NoError(t, err)
	if current != nil {
		integrationID = current.ID
	}
	body := map[string]interface{}{"integration_id": integrationID, "channel_actions": next}
	if previous != nil {
		body["previous_channel_actions"] = previous
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/integrations/slack/channels", strings.NewReader(
		string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email: user.Email,
		Name:  user.Name,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)
	handler.UpdateSlackChannelsHandler(c)
	return w
}

func TestEnforceIntegrationPermission_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

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
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	var result map[string]string
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "authentication required", result["error"])
}

func TestEnforceIntegrationPermission_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("user@example.com", "Regular User", []string{"developer"})
	require.NoError(t, err)

	handler := &AdminHandler{
		rbac:     newDenyAllRBAC(),
		database: database,
		config:   &config.Config{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
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
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "insufficient permissions", result["error"])
}

func TestLoadSlackIntegration_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

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
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	var result map[string]string
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, "no Slack integration found", result["error"])
}

func TestCreateSlackClient_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	handler := &AdminHandler{
		database: database,
		config:   &config.Config{},
	}

	// A stored token that can't be decrypted (here: plaintext written directly to the database)
	_, err = database.ReplaceSlackIntegration(
		"T123456", "Test Workspace", fakeSlackBot, "U123456", "channels:read,chat:write",
		map[string]interface{}{}, &user.ID,
	)
	require.NoError(t, err)
	_, err = database.DB().Exec(`UPDATE slack_integration SET bot_token_encrypted = 'xoxb-planted-token'`)
	require.NoError(t, err)
	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.Error(t, integration.BotTokenErr)
	require.Empty(t, integration.BotToken)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/test", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	client := handler.createSlackClient(c, integration)
	require.Nil(t, client)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
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
