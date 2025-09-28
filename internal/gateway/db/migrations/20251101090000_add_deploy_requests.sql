-- Deploy requests for approval-gated deploy flows

CREATE TABLE IF NOT EXISTS deploy_requests (
  id BIGSERIAL PRIMARY KEY,
  rack VARCHAR(120) NOT NULL,
  message TEXT NOT NULL,
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
  build_id VARCHAR(120),
  build_created_at TIMESTAMPTZ,
  object_key VARCHAR(255),
  object_created_at TIMESTAMPTZ,
  release_id VARCHAR(120),
  release_created_at TIMESTAMPTZ,
  release_promoted_at TIMESTAMPTZ,
  release_promoted_by_api_token_id BIGINT REFERENCES api_tokens(id) ON DELETE SET NULL,
  approval_notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_deploy_requests_token ON deploy_requests(target_api_token_id);
CREATE INDEX IF NOT EXISTS idx_deploy_requests_status ON deploy_requests(status);
CREATE INDEX IF NOT EXISTS idx_deploy_requests_approved_at ON deploy_requests(approved_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_requests_updated_at ON deploy_requests(updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_deploy_requests_active ON deploy_requests(target_api_token_id) WHERE status IN ('pending','approved');
