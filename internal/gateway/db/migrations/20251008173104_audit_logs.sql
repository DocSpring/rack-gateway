-- Audit logs table with event_count and API token metadata
CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGSERIAL PRIMARY KEY,
  timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  user_email VARCHAR(254) NOT NULL,
  user_name VARCHAR(200),
  action_type VARCHAR(100) NOT NULL,
  action VARCHAR(150) NOT NULL,
  command TEXT CHECK (char_length(command) <= 2000),
  resource VARCHAR(255),
  resource_type VARCHAR(100),
  details TEXT CHECK (char_length(details) <= 8000),
  ip_address INET,
  user_agent VARCHAR(512),
  status VARCHAR(32) NOT NULL,
  rbac_decision VARCHAR(16),
  http_status INTEGER,
  response_time_ms INTEGER NOT NULL DEFAULT 0,
  event_count INTEGER NOT NULL DEFAULT 1,
  api_token_id BIGINT REFERENCES api_tokens(id) ON DELETE SET NULL,
  api_token_name VARCHAR(150)
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_email ON audit_logs(user_email);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action_type ON audit_logs(action_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_status ON audit_logs(status);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_type ON audit_logs(resource_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_timestamp ON audit_logs(user_email, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_status_action_resource_ts ON audit_logs(status, action_type, resource_type, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_event ON audit_logs(user_email, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_api_token_id ON audit_logs(api_token_id);
