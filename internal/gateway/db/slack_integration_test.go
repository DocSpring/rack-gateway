package db_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestSlackIntegrationCRUD(t *testing.T) {
	database := dbtest.NewDatabase(t)

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
	require.NotNil(t, integration)
	require.Equal(t, "T123456", integration.WorkspaceID)
	require.Equal(t, "Test Workspace", integration.WorkspaceName)
	require.Equal(t, botToken, integration.BotToken)
	require.Equal(t, user.ID, *integration.CreatedByUserID)
	require.NotEmpty(t, integration.ChannelActions)

	// Test Get
	retrieved, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, integration.ID, retrieved.ID)
	require.Equal(t, integration.WorkspaceID, retrieved.WorkspaceID)
	require.NoError(t, retrieved.BotTokenErr)
	require.Equal(t, botToken, retrieved.BotToken)

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

	err = database.UpdateSlackIntegrationChannels(integration.ID, channelActions, updatedActions)
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

// Reconnecting Slack replaces the integration, so there's only ever one, even when two reconnects run at once. A
// replacement that can't be stored keeps the integration there was.
func TestReplaceSlackIntegrationKeepsOne(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)
	replace := func(workspaceID, name string) error {
		_, err := database.ReplaceSlackIntegration(workspaceID, name, fakeSlackBot, "U123456", "chat:write",
			map[string]interface{}{}, &user.ID)
		return err
	}

	require.NoError(t, replace("T1", "First"))
	require.NoError(t, replace("T1", "First, reconnected"))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, workspaceID := range []string{"T2", "T3"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- replace(workspaceID, "Workspace "+workspaceID)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, database.DB().QueryRow("SELECT COUNT(*) FROM slack_integration").Scan(&count))
	require.Equal(t, 1, count)

	current, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.Error(t, replace("T4", strings.Repeat("x", 300)), "the name is longer than its column")
	after, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.NotNil(t, after, "the failed reconnect kept the integration there was")
	require.Equal(t, current.WorkspaceID, after.WorkspaceID)
}

func TestUpdateSlackChannels_NoIntegration(t *testing.T) {
	database := dbtest.NewDatabase(t)

	err := database.UpdateSlackIntegrationChannels(1, map[string]interface{}{}, map[string]interface{}{})
	require.ErrorIs(t, err, db.ErrNoSlackIntegration)
}

// A change made to an older configuration is refused, so it can't undo a save made since.
func TestUpdateSlackChannels_RefusesAChangeToAnOlderConfiguration(t *testing.T) {
	database := dbtest.NewDatabase(t)
	channel := func(actions ...string) map[string]interface{} {
		return map[string]interface{}{"security": map[string]interface{}{
			"id": "C123456", "name": "security", "actions": actions,
		}}
	}
	si, err := database.ReplaceSlackIntegration("T123456", "Test Workspace", fakeSlackBot, "U123456",
		"channels:read,chat:write", channel("login.*_failed"), nil)
	require.NoError(t, err)

	// Two edits made to the same configuration: the first saves, the second is refused.
	require.NoError(t, database.UpdateSlackIntegrationChannels(si.ID,
		channel("login.*_failed"), channel("login.*_failed", "audit_log.verification_*")))
	err = database.UpdateSlackIntegrationChannels(si.ID, channel("login.*_failed"), channel())
	require.ErrorIs(t, err, db.ErrSlackChannelsChanged)

	stored, err := database.GetSlackIntegration()
	require.NoError(t, err)
	actions := stored.ChannelActions["security"].(map[string]interface{})["actions"]
	require.Equal(t, []interface{}{"login.*_failed", "audit_log.verification_*"}, actions)

	// The configuration as read now is accepted, whatever its key order.
	reordered := map[string]interface{}{"security": map[string]interface{}{
		"name": "security", "actions": []string{"login.*_failed", "audit_log.verification_*"}, "id": "C123456",
	}}
	require.NoError(t, database.UpdateSlackIntegrationChannels(si.ID, reordered, channel("mfa.verify_failed")))

	// After a reconnect, an edit made to the replaced integration is refused, even with the same channels.
	reconnected, err := database.ReplaceSlackIntegration("T999999", "New Workspace", fakeSlackBot, "U999999",
		"channels:read,chat:write", channel("mfa.verify_failed"), nil)
	require.NoError(t, err)
	err = database.UpdateSlackIntegrationChannels(si.ID, channel("mfa.verify_failed"), channel())
	require.ErrorIs(t, err, db.ErrSlackChannelsChanged)
	require.NoError(t, database.UpdateSlackIntegrationChannels(reconnected.ID, channel("mfa.verify_failed"), channel()))
}

// A disconnect waits for a reconnect in progress, so it disconnects the integration the reconnect stores rather
// than reporting success with it connected.
func TestDeleteSlackIntegrationWaitsForAReconnect(t *testing.T) {
	database := dbtest.NewDatabase(t)
	_, err := database.ReplaceSlackIntegration("T1", "First", fakeSlackBot, "U1", "chat:write",
		map[string]interface{}{}, nil)
	require.NoError(t, err)

	// A reconnect holds the lock and has deleted the old row.
	reconnect, err := database.DB().Begin()
	require.NoError(t, err)
	defer func() { _ = reconnect.Rollback() }()
	_, err = reconnect.Exec("SELECT pg_advisory_xact_lock($1)", db.AdvisoryLockSlackIntegration)
	require.NoError(t, err)
	_, err = reconnect.Exec("DELETE FROM slack_integration")
	require.NoError(t, err)

	disconnected := make(chan error, 1)
	go func() { disconnected <- database.DeleteSlackIntegration() }()
	select {
	case err := <-disconnected:
		t.Fatalf("the disconnect didn't wait for the reconnect: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	_, err = reconnect.Exec(`INSERT INTO slack_integration (workspace_id, workspace_name, bot_token_encrypted)
		VALUES ('T2', 'Second', 'token')`)
	require.NoError(t, err)
	require.NoError(t, reconnect.Commit())
	require.NoError(t, <-disconnected)

	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.Nil(t, integration, "the reconnected integration was disconnected")
	require.ErrorIs(t, database.UpdateSlackAlertSettings(true, "C1"), db.ErrNoSlackIntegration)
}

func TestDeleteSlackIntegration_NoIntegration(t *testing.T) {
	database := dbtest.NewDatabase(t)

	// Should not error when there's nothing to delete
	err := database.DeleteSlackIntegration()
	require.NoError(t, err)
}
