-- Add multifactor authentication schema support

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS mfa_enrolled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS mfa_enforced_at TIMESTAMPTZ;

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

ALTER TABLE user_sessions
    ADD COLUMN IF NOT EXISTS mfa_verified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS recent_step_up_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS trusted_device_id BIGINT REFERENCES trusted_devices(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS channel VARCHAR(20) NOT NULL DEFAULT 'web',
    ADD COLUMN IF NOT EXISTS device_id UUID,
    ADD COLUMN IF NOT EXISTS device_name VARCHAR(150),
    ADD COLUMN IF NOT EXISTS device_metadata JSONB;

CREATE INDEX IF NOT EXISTS idx_user_sessions_channel ON user_sessions(user_id, channel);

-- Trusted device and MFA global settings
INSERT INTO settings (key, value, updated_at)
VALUES ('mfa', '{"require_all_users": true, "trusted_device_ttl_days": 30, "step_up_window_minutes": 10}'::jsonb, NOW())
ON CONFLICT (key) DO NOTHING;

ALTER TABLE cli_login_states
    ADD COLUMN IF NOT EXISTS code_verifier TEXT,
    ADD COLUMN IF NOT EXISTS login_token TEXT,
    ADD COLUMN IF NOT EXISTS login_email VARCHAR(254),
    ADD COLUMN IF NOT EXISTS login_name VARCHAR(200),
    ADD COLUMN IF NOT EXISTS login_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mfa_verified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mfa_method_id BIGINT,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE cli_login_states
    ALTER COLUMN code DROP NOT NULL;
