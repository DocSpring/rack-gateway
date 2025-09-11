-- Settings table to store gateway settings as JSONB
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL
);

