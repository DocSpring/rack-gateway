package slack

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// testGateway is the gateway the tests' notifiers name in their messages.
var testGateway = config.NewGatewayIdentity("Staging", "https://gateway.example.com")

// testGatewayBlock is the first block of every message a testGateway notifier sends.
var testGatewayBlock = map[string]interface{}{
	"type": "section",
	"text": map[string]interface{}{
		"type": "mrkdwn",
		"text": "*<https://gateway.example.com|Rack Gateway (Staging)>*",
	},
}

func sectionText(t *testing.T, block map[string]interface{}) string {
	t.Helper()
	text, ok := block["text"].(map[string]interface{})
	require.True(t, ok, "block has no text: %v", block)
	s, ok := text["text"].(string)
	require.True(t, ok, "block text is not a string: %v", block)
	return s
}

func TestFormatAuditLogMessageNamesTheGatewayFirst(t *testing.T) {
	notifier := NewNotifier(nil, testGateway)
	action := audit.BuildAction(rbac.ResourceAPIToken.String(), "created")
	auditLog := &db.AuditLog{
		Action:       action,
		UserEmail:    "admin@example.com",
		UserName:     "Admin",
		Resource:     "deploy-bot",
		ResourceType: "api_token",
		Status:       audit.StatusSuccess,
		Timestamp:    time.Now(),
	}

	text, blocks := notifier.formatAuditLogMessage(auditLog)

	require.Equal(t, "Rack Gateway (Staging): 🔑 *"+action+"* - Admin (admin@example.com)", text)
	require.Equal(t, testGatewayBlock, blocks[0])
	require.Equal(t, "*🔑 "+action+"*", sectionText(t, blocks[1]))

	fields, ok := blocks[2]["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "*User:*\nAdmin (admin@example.com)", fields[0]["text"])
	require.Equal(t, "*Resource:*\ndeploy-bot (api_token)", fields[1]["text"])
}

func TestFormatDeployApprovalAlertNamesTheGatewayFirst(t *testing.T) {
	notifier := NewNotifier(nil, testGateway)
	req := &db.DeployApprovalRequest{
		PublicID:      "req_abc123",
		App:           "myapp",
		GitBranch:     "main",
		GitCommitHash: "abc123def456789",
		Message:       "Ship it",
		CreatedAt:     time.Now(),
	}

	text, blocks := notifier.formatDeployApprovalAlert(req)

	require.Equal(t, "Rack Gateway (Staging): 🚀 Deploy approval needed for main - Ship it", text)
	require.Equal(t, testGatewayBlock, blocks[0])
	require.Equal(t, deployApprovalHeaderBlock("main"), blocks[1])
}

func TestGatewayIsNamedWithoutRackNameOrURL(t *testing.T) {
	notifier := NewNotifier(nil, config.GatewayIdentity{})

	text, blocks := notifier.formatAuditLogMessage(&db.AuditLog{
		Action:    "user.create",
		UserEmail: "admin@example.com",
		Status:    audit.StatusSuccess,
	})

	require.Equal(t, "Rack Gateway: 📝 *user.create* - admin@example.com", text)
	require.Equal(t, "*Rack Gateway*", sectionText(t, blocks[0]))
}

func TestGatewayNameIsEscapedForMrkdwn(t *testing.T) {
	notifier := NewNotifier(nil, config.NewGatewayIdentity("R&D <EU>", "https://gw.example.com"))

	text, blocks := notifier.formatDeployApprovalAlert(&db.DeployApprovalRequest{GitBranch: "main", Message: "Ship it"})

	require.Equal(t, "Rack Gateway (R&amp;D &lt;EU&gt;): 🚀 Deploy approval needed for main - Ship it", text)
	require.Equal(t, "*<https://gw.example.com|Rack Gateway (R&amp;D &lt;EU&gt;)>*", sectionText(t, blocks[0]))
}

func TestTestMessageNamesTheGateway(t *testing.T) {
	text, blocks := TestMessage(testGateway)

	require.Equal(t, "Rack Gateway (Staging): 🧪 Test notification", text)
	require.Len(t, blocks, 2)
	require.Equal(t, testGatewayBlock, blocks[0])
	require.Equal(t, "🧪 Test notification", sectionText(t, blocks[1]))
}
