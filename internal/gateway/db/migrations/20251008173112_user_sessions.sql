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

-- Add foreign key constraint for user_sessions.trusted_device_id
ALTER TABLE user_sessions
  ADD CONSTRAINT fk_user_sessions_trusted_device
  FOREIGN KEY (trusted_device_id) REFERENCES trusted_devices(id) ON DELETE SET NULL;
