-- Core schema for Rack Gateway

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Users table with MFA support
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  email VARCHAR(254) NOT NULL UNIQUE,
  name VARCHAR(120) NOT NULL,
  roles TEXT NOT NULL CHECK (char_length(roles) <= 1024),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  suspended BOOLEAN NOT NULL DEFAULT FALSE,
  mfa_enrolled BOOLEAN NOT NULL DEFAULT FALSE,
  mfa_enforced_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- API tokens table with public_id
CREATE TABLE IF NOT EXISTS api_tokens (
  id BIGSERIAL PRIMARY KEY,
  public_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  token_hash CHAR(64) NOT NULL UNIQUE,
  name VARCHAR(150) NOT NULL UNIQUE,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  permissions TEXT NOT NULL CHECK (char_length(permissions) <= 4000),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_created_by ON api_tokens(created_by_user_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_public_id ON api_tokens(public_id);

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

-- CLI login states for OAuth PKCE flow
CREATE TABLE IF NOT EXISTS cli_login_states (
  state VARCHAR(200) PRIMARY KEY,
  code VARCHAR(200),
  code_verifier TEXT,
  login_token TEXT,
  login_email VARCHAR(254),
  login_name VARCHAR(200),
  login_expires_at TIMESTAMPTZ,
  login_error TEXT,
  mfa_verified_at TIMESTAMPTZ,
  mfa_method_id BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Settings table for global configuration
CREATE TABLE IF NOT EXISTS settings (
  key VARCHAR(100) PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL
);

-- MFA global settings
INSERT INTO settings (key, value, updated_at)
VALUES ('mfa', '{"require_all_users": true, "trusted_device_ttl_days": 30, "step_up_window_minutes": 10}'::jsonb, NOW())
ON CONFLICT (key) DO NOTHING;

-- User resources tracking
CREATE TABLE IF NOT EXISTS user_resources (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  resource_type VARCHAR(100) NOT NULL,
  resource_id VARCHAR(255) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_user_resources_user ON user_resources(user_id);
CREATE INDEX IF NOT EXISTS idx_user_resources_type ON user_resources(resource_type);

-- Internal metadata for environment tracking
CREATE TABLE IF NOT EXISTS rgw_internal_metadata (
  id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
  environment VARCHAR(32) NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (char_length(environment) <= 32)
);

-- User sessions for web authentication
CREATE TABLE IF NOT EXISTS user_sessions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash CHAR(64) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL,
  ip_address INET,
  user_agent VARCHAR(512),
  revoked_at TIMESTAMPTZ,
  revoked_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  metadata JSONB,
  mfa_verified_at TIMESTAMPTZ,
  recent_step_up_at TIMESTAMPTZ,
  trusted_device_id BIGINT,
  channel VARCHAR(20) NOT NULL DEFAULT 'web',
  device_id UUID,
  device_name VARCHAR(150),
  device_metadata JSONB,
  CONSTRAINT user_sessions_token_hash_length CHECK (char_length(token_hash) = 64)
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_active ON user_sessions(user_id, revoked_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_user_sessions_channel ON user_sessions(user_id, channel);

-- MFA methods
CREATE TABLE IF NOT EXISTS mfa_methods (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type VARCHAR(50) NOT NULL,
  label VARCHAR(150),
  secret TEXT,
  credential_id BYTEA,
  public_key BYTEA,
  transports JSONB,
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  confirmed_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  UNIQUE (user_id, type, credential_id)
);

CREATE INDEX IF NOT EXISTS idx_mfa_methods_user ON mfa_methods(user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_methods_type ON mfa_methods(type);

-- MFA backup codes
CREATE TABLE IF NOT EXISTS mfa_backup_codes (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash CHAR(64) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  used_at TIMESTAMPTZ,
  UNIQUE (user_id, code_hash)
);

CREATE INDEX IF NOT EXISTS idx_mfa_backup_codes_user ON mfa_backup_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_backup_codes_used ON mfa_backup_codes(user_id, used_at);

-- Trusted devices for MFA
CREATE TABLE IF NOT EXISTS trusted_devices (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id UUID NOT NULL,
  token_hash CHAR(64) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ip_first INET,
  ip_last INET,
  user_agent_hash CHAR(64),
  revoked_at TIMESTAMPTZ,
  revoked_reason TEXT,
  metadata JSONB,
  UNIQUE (user_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_trusted_devices_user ON trusted_devices(user_id);
CREATE INDEX IF NOT EXISTS idx_trusted_devices_active ON trusted_devices(user_id, expires_at) WHERE revoked_at IS NULL;

-- Add foreign key constraint for user_sessions.trusted_device_id
ALTER TABLE user_sessions
  ADD CONSTRAINT fk_user_sessions_trusted_device
  FOREIGN KEY (trusted_device_id) REFERENCES trusted_devices(id) ON DELETE SET NULL;

-- Deploy approval requests for approval-gated deploy flows
CREATE TABLE IF NOT EXISTS deploy_approval_requests (
  id BIGSERIAL PRIMARY KEY,
  message TEXT NOT NULL,
  app VARCHAR(255) NOT NULL,
  release_id VARCHAR(120) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','consumed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_by_api_token_id BIGINT REFERENCES api_tokens(id) ON DELETE SET NULL,
  target_api_token_id BIGINT NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,
  target_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  approved_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  approved_at TIMESTAMPTZ,
  rejected_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  rejected_at TIMESTAMPTZ,
  approval_expires_at TIMESTAMPTZ,
  release_created_at TIMESTAMPTZ,
  release_promoted_at TIMESTAMPTZ,
  release_promoted_by_api_token_id BIGINT REFERENCES api_tokens(id) ON DELETE SET NULL,
  approval_notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_token ON deploy_approval_requests(target_api_token_id);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_status ON deploy_approval_requests(status);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_approved_at ON deploy_approval_requests(approved_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_updated_at ON deploy_approval_requests(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_app_release ON deploy_approval_requests(app, release_id);

CREATE UNIQUE INDEX idx_deploy_approval_requests_active_release
  ON deploy_approval_requests(app, target_api_token_id, release_id)
  WHERE status IN ('pending','approved');
