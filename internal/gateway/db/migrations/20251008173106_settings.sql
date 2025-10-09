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

-- Approved commands setting (empty by default, must be configured)
INSERT INTO settings (key, value, updated_at)
VALUES ('approved_commands', '{"commands": []}'::jsonb, NOW())
ON CONFLICT (key) DO NOTHING;
