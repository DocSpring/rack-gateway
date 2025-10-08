# Slack Integration

Rack Gateway can send audit log notifications to Slack channels for security events, deploy approvals, and other important actions.

## Features

- **OAuth 2.0 Integration**: Secure connection to your Slack workspace
- **Flexible Channel Routing**: Route different event types to different channels
- **Glob Pattern Matching**: Use wildcards to match multiple action types (e.g., `mfa.*`, `deploy-approval-request.*`)
- **Rich Formatting**: Events are formatted with emojis, colors, and structured blocks for easy readability
- **Test Notifications**: Send test messages to verify channel configuration

## Setup

### 1. Create a Slack App

1. Go to [https://api.slack.com/apps](https://api.slack.com/apps)
2. Click **Create New App** → **From an app manifest**
3. Select your workspace
4. Use the manifest below (update `name` and `redirect_urls` as needed):

```json
{
  "display_information": {
    "name": "Rack Gateway",
    "description": "Security and deployment notifications",
    "background_color": "#2c2d30"
  },
  "features": {
    "bot_user": {
      "display_name": "Rack Gateway",
      "always_online": true
    }
  },
  "oauth_config": {
    "redirect_urls": [
      "https://your-gateway-domain.com/.gateway/api/admin/integrations/slack/oauth/callback"
    ],
    "scopes": {
      "bot": [
        "channels:read",
        "chat:write"
      ]
    }
  },
  "settings": {
    "org_deploy_enabled": false,
    "socket_mode_enabled": false,
    "is_hosted": false,
    "token_rotation_enabled": false
  }
}
```

5. Click **Create**
6. Go to **OAuth & Permissions** to find your credentials

### 2. Configure Environment Variables

Add the following to your gateway configuration:

```bash
SLACK_CLIENT_ID="your-client-id-here"
SLACK_CLIENT_SECRET="your-client-secret-here"
```

**Important**: Restart the gateway after setting these variables.

### 3. Connect via Web UI

1. Log in to the gateway web UI as an admin
2. Navigate to **Integrations**
3. Click **Connect to Slack**
4. Authorize the app in your Slack workspace
5. Configure channel routing (see below)

## Channel Configuration

### Default Channels

The integration creates two default channel configurations:

**#security** - Security-related events:
- `login.complete` - Successful login attempts
- `login.*_failed` - Failed login attempts (oauth_failed, user_not_authorized, etc.)
- `mfa.*` - MFA enrollment, verification, backup code usage
- `user.update_roles` - User role changes
- `api-token.*` - API token creation, updates, deletion

**#infrastructure** - Deployment and infrastructure events:
- `deploy-approval-request.*` - Deploy approval requests, approvals, rejections
- `release.promote` - Release promotions

### Customizing Channels

You can add, remove, or modify channel configurations through the web UI:

1. **Add a new channel**:
   - Click **Add Channel**
   - Enter a name (e.g., `#security`, `#dev-ops`)
   - Select the Slack channel from the dropdown
   - Add action patterns

2. **Add action patterns**:
   - Use glob patterns like `mfa.*` to match all MFA events
   - Or specific actions like `deploy-approval-request.approved`
   - Multiple patterns are supported per channel

3. **Test notifications**:
   - Click **Test** next to any configured channel
   - A test message will be sent immediately

### Available Actions

View the **Audit Logs** page to see all available action types. Common patterns:

| Pattern | Matches |
|---------|---------|
| `mfa.*` | All MFA events (enroll, verify, backup-code-used) |
| `auth.*` | All authentication events (login, logout, failed) |
| `api-token.*` | All API token events (created, updated, deleted) |
| `user.role.*` | User role changes (added, removed) |
| `deploy-approval-request.*` | All deploy approval events |
| `release.promote` | Release promotions |
| `*.failed` | Any failed action |
| `*.denied` | Any denied action |

## Message Formatting

Messages include:

- **Emoji indicators**: Different emojis for different event types
  - 🔐 MFA events
  - 🔑 Authentication and API tokens
  - 🚀 Deploy approvals
  - 👤 User role changes
  - 🚨 Failed/denied actions
  - ❌ Errors

- **Structured blocks**: Rich formatting with:
  - Action type
  - User information
  - Status (success, denied, error)
  - Timestamp
  - Details (when available)
  - IP address and user agent (when available)

## Security Considerations

- **Bot Token Security**: Bot tokens are encrypted before storage in the database
- **Admin-Only**: Only users with the `admin` role can connect or disconnect integrations
- **Audit Trail**: All integration changes are logged in the audit log
- **OAuth State Validation**: CSRF protection on OAuth flow

## Troubleshooting

### "No integrations configured" message

- Ensure `SLACK_CLIENT_ID` and `SLACK_CLIENT_SECRET` are set
- Restart the gateway after setting environment variables
- Check logs for configuration errors

### "Failed to start Slack authorization"

- Verify the redirect URL in your Slack app matches your gateway domain
- Check that the gateway is accessible at the configured domain
- Ensure OAuth credentials are correct

### Messages not appearing in Slack

1. **Check channel configuration**:
   - Verify the channel is selected in the dropdown
   - Ensure action patterns match the events you want (check Audit Logs for action names)

2. **Verify bot permissions**:
   - Bot must be invited to the channel (`/invite @Rack Gateway`)
   - Bot needs `chat:write` permission

3. **Test the integration**:
   - Use the **Test** button to send a test message
   - Check Sentry for any notification errors (if Sentry is configured)

### Bot not appearing in channel list

- Only **public channels** are shown in the dropdown
- For private channels, invite the bot first: `/invite @Rack Gateway`
- Verify the `channels:read` scope is granted
- Refresh the gateway page to reload the channel list

## Database Schema

The integration uses a single table:

```sql
CREATE TABLE slack_integration (
  id BIGSERIAL PRIMARY KEY,
  workspace_id VARCHAR(255) NOT NULL UNIQUE,
  workspace_name VARCHAR(255),
  bot_token_encrypted TEXT NOT NULL,
  channel_actions JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  bot_user_id VARCHAR(255),
  scope TEXT
);
```

Channel actions are stored as JSONB:

```json
{
  "security": {
    "id": "C123456",
    "name": "#security",
    "actions": ["mfa.*", "auth.*"]
  },
  "infrastructure": {
    "id": "C789012",
    "name": "#infrastructure",
    "actions": ["deploy-approval-request.*"]
  }
}
```

## API Endpoints

All endpoints require admin authentication:

- `POST /.gateway/api/admin/integrations/slack/oauth/authorize` - Start OAuth flow
- `GET /.gateway/api/admin/integrations/slack/oauth/callback` - OAuth callback
- `GET /.gateway/api/admin/integrations/slack` - Get integration status
- `PUT /.gateway/api/admin/integrations/slack/channels` - Update channel configuration
- `DELETE /.gateway/api/admin/integrations/slack` - Disconnect integration
- `GET /.gateway/api/admin/integrations/slack/channels/list` - List available channels
- `POST /.gateway/api/admin/integrations/slack/test` - Send test notification

## Limitations

- **Single Workspace**: Only one Slack workspace can be connected at a time
- **Bot Tokens**: Uses bot tokens (not user tokens) for posting messages
- **Public Channels Only**: Bot can only see and post to public channels (unless explicitly invited to private channels)
- **No Interactive Features**: Currently send-only (no buttons or interactive components)

## Future Enhancements

Potential improvements for future versions:

- Interactive buttons for deploy approvals
- Support for multiple workspaces
- Slash commands for common operations
- Message threading for related events
- Direct message notifications for user-specific events
