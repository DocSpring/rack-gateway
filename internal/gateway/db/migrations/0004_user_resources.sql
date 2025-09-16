-- Record creators for resources (apps, builds, releases)
CREATE TABLE IF NOT EXISTS user_resources (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_user_resources_user ON user_resources(user_id);
CREATE INDEX IF NOT EXISTS idx_user_resources_type ON user_resources(resource_type);

