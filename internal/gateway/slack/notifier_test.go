package slack

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestNotifyAuditEvent_NoIntegration(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	notifier := NewNotifier(database)

	auditLog := &db.AuditLog{
		Action:    "mfa.enroll",
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
			"actions": []string{"deploy.*"}, // Won't match mfa.enroll
		},
	}
	botToken := base64.StdEncoding.EncodeToString([]byte("xoxb-test-token"))
	_, err = database.CreateSlackIntegration("T123", "Test", botToken, "U123", "channels:read,chat:write", channelActions, &user.ID)
	require.NoError(t, err)

	notifier := NewNotifier(database)

	auditLog := &db.AuditLog{
		Action:    "mfa.enroll",
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
		{"mfa.*", "mfa.enroll", true},
		{"mfa.*", "mfa.verify", true},
		{"mfa.*", "auth.login", false},
		{"auth.*", "auth.login", true},
		{"auth.*", "auth.logout", true},
		{"deploy-approval-request.*", "deploy-approval-request.created", true},
		{"deploy-approval-request.*", "deploy-approval-request.approved", true},
		{"deploy-approval-request.*", "mfa.enroll", false},
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
			"actions": []interface{}{"mfa.*", "auth.*", "api-token.*"},
		},
		"infrastructure": map[string]interface{}{
			"id":      "C222",
			"name":    "#infrastructure",
			"actions": []interface{}{"deploy-approval-request.*", "release.promote", "*.created"},
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
		{"mfa.enroll", []string{"C111"}},
		{"auth.login", []string{"C111"}},
		{"api-token.created", []string{"C111", "C222"}}, // Matches both security and infrastructure (*.created)
		{"deploy-approval-request.created", []string{"C222"}},
		{"release.promote", []string{"C222"}},
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
				Action:    "mfa.enroll",
				UserEmail: "user@example.com",
				UserName:  "Test User",
				Status:    "success",
				Timestamp: time.Now(),
				Details:   "TOTP enrolled",
			},
			expectEmoji:   "🔐",
			expectInText:  []string{"mfa.enroll", "user@example.com"},
			expectInBlock: []string{"Test User", "success", "TOTP enrolled"},
		},
		{
			name: "OAuth failed",
			auditLog: &db.AuditLog{
				Action:    "login.oauth_failed",
				UserEmail: "hacker@example.com",
				Status:    "failed",
				IPAddress: "192.168.1.1",
				Timestamp: time.Now(),
			},
			expectEmoji:   "🚨",
			expectInText:  []string{"login.oauth_failed", "hacker@example.com"},
			expectInBlock: []string{"failed", "192.168.1.1"},
		},
		{
			name: "Deploy approval request",
			auditLog: &db.AuditLog{
				Action:    "deploy-approval-request.created",
				UserEmail: "deployer@example.com",
				UserName:  "CI Bot",
				Status:    "success",
				Timestamp: time.Now(),
				Details:   "Release R123 for app my-app",
			},
			expectEmoji:   "🚀",
			expectInText:  []string{"deploy-approval-request.created"},
			expectInBlock: []string{"CI Bot", "deployer@example.com", "success", "Release R123"},
		},
		{
			name: "API token created",
			auditLog: &db.AuditLog{
				Action:       "api-token.created",
				UserEmail:    "admin@example.com",
				UserName:     "Admin",
				APITokenName: "ci-token",
				Status:       "success",
				Timestamp:    time.Now(),
			},
			expectEmoji:   "🔑",
			expectInText:  []string{"api-token.created", "API Token: ci-token"},
			expectInBlock: []string{"success"},
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
