-- Add Slack integration table
CREATE TABLE IF NOT EXISTS slack_integration (
  id BIGSERIAL PRIMARY KEY,
  workspace_id VARCHAR(255) NOT NULL UNIQUE,
  workspace_name VARCHAR(255),
  bot_token_encrypted TEXT NOT NULL,

  -- Maps Slack channels to arrays of audit log action patterns
  -- Example: {
  --   "C123ABC": {
  --     "name": "#security",
  --     "actions": ["mfa.*", "user.role.*", "api-token.*", "auth.failed"]
  --   },
  --   "C456DEF": {
  --     "name": "#infrastructure",
  --     "actions": ["deploy-approval-request.*", "release.promote"]
  --   }
  -- }
  channel_actions JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,

  -- OAuth metadata
  bot_user_id VARCHAR(255),
  scope TEXT
);

CREATE INDEX IF NOT EXISTS idx_slack_integration_workspace ON slack_integration(workspace_id);
