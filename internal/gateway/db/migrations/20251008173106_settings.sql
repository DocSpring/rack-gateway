-- Unified settings table for global and app-specific configuration
-- app_name NULL = global setting
-- app_name non-null = app-specific setting
CREATE TABLE IF NOT EXISTS settings (
  id BIGSERIAL PRIMARY KEY,
  app_name VARCHAR(255),
  key VARCHAR(255) NOT NULL,
  value JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  UNIQUE(app_name, key)
);

-- Index for efficient lookups
CREATE INDEX IF NOT EXISTS idx_settings_app_key ON settings(app_name, key);
