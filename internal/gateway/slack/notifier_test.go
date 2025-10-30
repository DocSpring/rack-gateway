package slack

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestNotifyAuditEvent_NoIntegration(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	notifier := NewNotifier(database)

	auditLog := &db.AuditLog{
		Action:    audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),
		UserEmail: "test@example.com",
		Status:    "success",
		Timestamp: time.Now(),
	}

	// Should not error when no integration exists
	err := notifier.NotifyAuditEvent(auditLog)
	require.NoError(t, err)
}

func TestNotifyAuditEvent_NoMatchingChannels(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create user
	user, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create integration with no matching patterns
	channelActions := map[string]interface{}{
		"security": map[string]interface{}{
			"id":      "C123456",
			"name":    "#security",
			"actions": []string{"deploy.*"}, // Won't match mfa_method.enroll
		},
	}
	botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
	_, err = database.CreateSlackIntegration(
		"T123",
		"Test",
		botToken,
		"U123",
		"channels:read,chat:write",
		channelActions,
		&user.ID,
	)
	require.NoError(t, err)

	notifier := NewNotifier(database)

	auditLog := &db.AuditLog{
		Action:    audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),
		UserEmail: "test@example.com",
		Status:    "success",
		Timestamp: time.Now(),
	}

	// Should not error when no channels match
	err = notifier.NotifyAuditEvent(auditLog)
	require.NoError(t, err)
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"mfa_method.*", audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll), true},
		{"mfa_method.*", audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbVerify), true},
		{"mfa_method.*", "auth.login", false},
		{"auth.*", "auth.login", true},
		{"auth.*", "auth.logout", true},
		{"deploy-approval-request.*", "deploy-approval-request.created", true},
		{"deploy-approval-request.*", "deploy-approval-request.approved", true},
		{"deploy-approval-request.*", audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll), false},
		{"*.created", "deploy-approval-request.created", true},
		{"*.created", "api-token.created", true},
		{"*.created", "deploy-approval-request.approved", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.text, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.text)
			require.Equal(t, tt.want, got, "matchGlob(%q, %q)", tt.pattern, tt.text)
		})
	}
}

func TestMatchActionToChannels(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	notifier := NewNotifier(database)

	channelActions := map[string]interface{}{
		"security": map[string]interface{}{
			"id":      "C111",
			"name":    "#security",
			"actions": []interface{}{"mfa_method.*", "auth.*", "api-token.*"},
		},
		"infrastructure": map[string]interface{}{
			"id":   "C222",
			"name": "#infrastructure",
			"actions": []interface{}{
				"deploy-approval-request.*",
				audit.BuildAction(rbac.ResourceRelease.String(), rbac.ActionPromote.String()),
				"*.created",
			},
		},
		"no-id": map[string]interface{}{
			"id":      nil,
			"name":    "#no-channel",
			"actions": []interface{}{"*.created"},
		},
	}

	tests := []struct {
		action   string
		expected []string
	}{
		{audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll), []string{"C111"}},
		{"auth.login", []string{"C111"}},
		{"api-token.created", []string{"C111", "C222"}}, // Matches both security and infrastructure (*.created)
		{"deploy-approval-request.created", []string{"C222"}},
		{audit.BuildAction(rbac.ResourceRelease.String(), rbac.ActionPromote.String()), []string{"C222"}},
		{"unknown.action", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			channels := notifier.matchActionToChannels(tt.action, channelActions)
			require.ElementsMatch(t, tt.expected, channels, "matchActionToChannels(%q)", tt.action)
		})
	}
}

func TestFormatAuditLogMessage(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	notifier := NewNotifier(database)

	tests := []struct {
		name          string
		auditLog      *db.AuditLog
		expectEmoji   string
		expectInText  []string
		expectInBlock []string
	}{
		{
			name: "MFA enrollment success",
			auditLog: &db.AuditLog{
				Action:    audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),
				UserEmail: "user@example.com",
				UserName:  "Test User",
				Status:    audit.StatusSuccess,
				Timestamp: time.Now(),
				Details:   "TOTP enrolled",
			},
			expectEmoji: "🔐",
			expectInText: []string{
				audit.BuildAction(audit.ActionScopeMFAMethod, audit.ActionVerbEnroll),
				"user@example.com",
			},
			expectInBlock: []string{"Test User", audit.StatusSuccess, "TOTP enrolled"},
		},
		{
			name: "OAuth failed",
			auditLog: &db.AuditLog{
				Action:    audit.BuildAction(audit.ActionScopeLogin, audit.ActionVerbOAuthFailed),
				UserEmail: "hacker@example.com",
				Status:    audit.StatusFailed,
				IPAddress: "192.168.1.1",
				Timestamp: time.Now(),
			},
			expectEmoji: "🚨",
			expectInText: []string{
				audit.BuildAction(audit.ActionScopeLogin, audit.ActionVerbOAuthFailed),
				"hacker@example.com",
			},
			expectInBlock: []string{audit.StatusFailed, "192.168.1.1"},
		},
		{
			name: "Deploy approval request",
			auditLog: &db.AuditLog{
				Action:    audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), "created"),
				UserEmail: "deployer@example.com",
				UserName:  "CI Bot",
				Status:    audit.StatusSuccess,
				Timestamp: time.Now(),
				Details:   "Release R123 for app my-app",
			},
			expectEmoji:   "🚀",
			expectInText:  []string{audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), "created")},
			expectInBlock: []string{"CI Bot", "deployer@example.com", audit.StatusSuccess, "Release R123"},
		},
		{
			name: "API token created",
			auditLog: &db.AuditLog{
				Action:       audit.BuildAction(rbac.ResourceAPIToken.String(), "created"),
				UserEmail:    "admin@example.com",
				UserName:     "Admin",
				APITokenName: "ci-token",
				Status:       audit.StatusSuccess,
				Timestamp:    time.Now(),
			},
			expectEmoji: "🔑",
			expectInText: []string{
				audit.BuildAction(rbac.ResourceAPIToken.String(), "created"),
				"API Token: ci-token",
			},
			expectInBlock: []string{audit.StatusSuccess},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, blocks := notifier.formatAuditLogMessage(tt.auditLog)

			require.Contains(t, text, tt.expectEmoji)
			for _, s := range tt.expectInText {
				require.Contains(t, text, s)
			}

			blocksJSON, err := json.Marshal(blocks)
			require.NoError(t, err)
			blocksStr := string(blocksJSON)

			for _, s := range tt.expectInBlock {
				require.Contains(t, blocksStr, s)
			}
		})
	}
}

func TestNotifyDeployApprovalCreated(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	t.Run("skips when no integration configured", func(t *testing.T) {
		notifier := NewNotifier(database)
		req := &db.DeployApprovalRequest{
			App:           "myapp",
			GitBranch:     "feature-branch",
			GitCommitHash: "abc123def456",
			Message:       "Deploy feature X",
		}

		err := notifier.NotifyDeployApprovalCreated(req, "gateway.example.com")
		require.NoError(t, err)
	})

	t.Run("skips when alerts disabled", func(t *testing.T) {
		notifier := NewNotifier(database)

		// Create user for integration
		user, err := database.CreateUser("admin1@example.com", "Admin User", []string{"admin"})
		require.NoError(t, err)

		// Create integration with alerts disabled
		botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
		_, err = database.CreateSlackIntegration(
			"T123456-1",
			"Test Team 1",
			botToken,
			"U123456-1",
			"channels:read,chat:write",
			map[string]interface{}{},
			&user.ID,
		)
		require.NoError(t, err)

		req := &db.DeployApprovalRequest{
			App:           "myapp",
			GitBranch:     "feature-branch",
			GitCommitHash: "abc123def456",
			Message:       "Deploy feature X",
		}

		err = notifier.NotifyDeployApprovalCreated(req, "gateway.example.com")
		require.NoError(t, err)
	})

	t.Run("skips when channel not configured", func(t *testing.T) {
		notifier := NewNotifier(database)

		// Create user for integration
		user, err := database.CreateUser("admin2@example.com", "Admin User", []string{"admin"})
		require.NoError(t, err)

		// Create integration with alerts enabled but no channel
		botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
		_, err = database.CreateSlackIntegration(
			"T123456-2",
			"Test Team 2",
			botToken,
			"U123456-2",
			"channels:read,chat:write",
			map[string]interface{}{},
			&user.ID,
		)
		require.NoError(t, err)

		// Enable alerts but leave channel blank
		err = database.UpdateSlackAlertSettings(true, "")
		require.NoError(t, err)

		req := &db.DeployApprovalRequest{
			App:           "myapp",
			GitBranch:     "feature-branch",
			GitCommitHash: "abc123def456",
			Message:       "Deploy feature X",
		}

		err = notifier.NotifyDeployApprovalCreated(req, "gateway.example.com")
		require.NoError(t, err)
	})
}

func TestFormatDeployApprovalAlert(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	notifier := NewNotifier(database)

	t.Run("formats basic message", func(t *testing.T) {
		now := time.Now()
		req := &db.DeployApprovalRequest{
			ID:            1,
			PublicID:      "req_abc123",
			App:           "myapp",
			GitBranch:     "feature-branch",
			GitCommitHash: "abc123def456789",
			Message:       "Deploy feature X",
			CreatedAt:     now,
		}

		text, blocks := notifier.formatDeployApprovalAlert(req, "gateway.example.com")

		// Check text contains key elements
		require.Contains(t, text, "feature-branch")
		require.Contains(t, text, "Deploy feature X")
		require.Contains(t, text, "🚀")

		// Check blocks structure
		blocksJSON, err := json.Marshal(blocks)
		require.NoError(t, err)
		blocksStr := string(blocksJSON)

		require.Contains(t, blocksStr, "feature-branch")
		require.Contains(t, blocksStr, "myapp")
		require.Contains(t, blocksStr, "abc123d") // First 7 chars of commit
		require.Contains(t, blocksStr, "gateway.example.com/app/deploy-approvals/req_abc123")
	})

	t.Run("formats message with PR and CI metadata", func(t *testing.T) {
		now := time.Now()
		ciMeta := map[string]interface{}{
			"build_url": "https://circleci.com/gh/org/repo/123",
		}
		ciMetaBytes, err := json.Marshal(ciMeta)
		require.NoError(t, err)

		req := &db.DeployApprovalRequest{
			ID:             1,
			PublicID:       "req_abc123",
			App:            "myapp",
			GitBranch:      "feature-branch",
			GitCommitHash:  "abc123def456789",
			Message:        "Deploy feature X",
			PrURL:          "https://github.com/org/repo/pull/42",
			CIMetadata:     ciMetaBytes,
			CreatedByEmail: "user@example.com",
			CreatedByName:  "Test User",
			CreatedAt:      now,
		}

		text, blocks := notifier.formatDeployApprovalAlert(req, "gateway.example.com")

		require.Contains(t, text, "feature-branch")

		blocksJSON, err := json.Marshal(blocks)
		require.NoError(t, err)
		blocksStr := string(blocksJSON)

		// Check all links are present
		require.Contains(t, blocksStr, "github.com/org/repo/pull/42")
		require.Contains(t, blocksStr, "circleci.com/gh/org/repo/123")
		require.Contains(t, blocksStr, "gateway.example.com/app/deploy-approvals/req_abc123")
		require.Contains(t, blocksStr, "Test User")
		require.Contains(t, blocksStr, "user@example.com")
	})

	t.Run("formats message with API token creator", func(t *testing.T) {
		now := time.Now()
		req := &db.DeployApprovalRequest{
			ID:                    1,
			PublicID:              "req_abc123",
			App:                   "myapp",
			GitBranch:             "feature-branch",
			GitCommitHash:         "abc123def456789",
			Message:               "Deploy feature X",
			CreatedByAPITokenName: "CI Token",
			CreatedAt:             now,
		}

		text, blocks := notifier.formatDeployApprovalAlert(req, "gateway.example.com")

		require.Contains(t, text, "feature-branch")

		blocksJSON, err := json.Marshal(blocks)
		require.NoError(t, err)
		blocksStr := string(blocksJSON)

		require.Contains(t, blocksStr, "CI Token")
		require.Contains(t, blocksStr, "API token")
	})

	t.Run("formats message without branch name", func(t *testing.T) {
		now := time.Now()
		req := &db.DeployApprovalRequest{
			ID:            1,
			PublicID:      "req_abc123",
			App:           "myapp",
			GitBranch:     "",
			GitCommitHash: "abc123def456789",
			Message:       "Deploy feature X",
			CreatedAt:     now,
		}

		text, blocks := notifier.formatDeployApprovalAlert(req, "gateway.example.com")

		// Should fall back to "unknown branch"
		require.Contains(t, text, "unknown branch")

		blocksJSON, err := json.Marshal(blocks)
		require.NoError(t, err)
		blocksStr := string(blocksJSON)

		require.Contains(t, blocksStr, "unknown branch")
	})
}
