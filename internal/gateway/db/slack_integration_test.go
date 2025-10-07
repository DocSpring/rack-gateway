package db_test

import (
	"encoding/base64"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestSlackIntegrationCRUD(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create user
	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Test Create
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
	require.NotNil(t, integration)
	require.Equal(t, "T123456", integration.WorkspaceID)
	require.Equal(t, "Test Workspace", integration.WorkspaceName)
	require.Equal(t, botToken, integration.BotTokenEncrypted)
	require.Equal(t, user.ID, *integration.CreatedByUserID)
	require.NotEmpty(t, integration.ChannelActions)

	// Test Get
	retrieved, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, integration.ID, retrieved.ID)
	require.Equal(t, integration.WorkspaceID, retrieved.WorkspaceID)

	// Test Update Channels
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

	err = database.UpdateSlackIntegrationChannels(updatedActions)
	require.NoError(t, err)

	retrieved, err = database.GetSlackIntegration()
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Len(t, retrieved.ChannelActions, 2)

	securityChannel := retrieved.ChannelActions["security"].(map[string]interface{})
	require.Equal(t, "C123456", securityChannel["id"])
	actions := securityChannel["actions"].([]interface{})
	require.Len(t, actions, 3)

	// Test Delete
	err = database.DeleteSlackIntegration()
	require.NoError(t, err)

	retrieved, err = database.GetSlackIntegration()
	require.NoError(t, err)
	require.Nil(t, retrieved)
}

func TestSlackIntegration_NoDuplicates(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	channelActions := map[string]interface{}{}
	botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))

	// Create first integration
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

	// Attempting to create another integration with the same workspace_id should fail
	_, err = database.CreateSlackIntegration(
		"T123456",
		"Test Workspace 2",
		botToken,
		"U123456",
		"channels:read,chat:write",
		channelActions,
		&user.ID,
	)
	require.Error(t, err)
}

func TestUpdateSlackChannels_NoIntegration(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	updatedActions := map[string]interface{}{}
	err := database.UpdateSlackIntegrationChannels(updatedActions)
	require.Error(t, err)
}

func TestDeleteSlackIntegration_NoIntegration(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Should not error when there's nothing to delete
	err := database.DeleteSlackIntegration()
	require.NoError(t, err)
}
