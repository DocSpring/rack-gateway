-- Deploy approval requests for git commit-based approval flows
CREATE TABLE IF NOT EXISTS deploy_approval_requests (
  id BIGSERIAL PRIMARY KEY,
  public_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,

  -- Git commit metadata (required at creation)
  git_commit_hash VARCHAR(40) NOT NULL,
  git_branch VARCHAR(255),
  pipeline_url TEXT,
  message TEXT NOT NULL,

  -- CI provider integration (optional)
  ci_provider VARCHAR(50),
  ci_metadata JSONB,

  -- Build/release tracking
  app VARCHAR(255) NOT NULL,
  object_url TEXT,
  build_id VARCHAR(120),
  release_id VARCHAR(120),

  -- Status and lifecycle
  status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','expired','deployed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  -- Creator tracking
  created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_by_api_token_id BIGINT REFERENCES api_tokens(id) ON DELETE SET NULL,
  target_api_token_id BIGINT NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,

  -- Approval tracking
  approved_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  approved_at TIMESTAMPTZ,
  approval_expires_at TIMESTAMPTZ,
  approval_notes TEXT,

  -- Rejection tracking
  rejected_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  rejected_at TIMESTAMPTZ
);

-- Deploy approval state machine trigger
-- Enforces strict ordering: object_url → build_id → release_id → deployed
CREATE OR REPLACE FUNCTION validate_deploy_approval_state_machine()
RETURNS TRIGGER AS $$
BEGIN
  -- Only validate updates (not inserts)
  IF TG_OP = 'INSERT' THEN
    RETURN NEW;
  END IF;

  -- Rule 1: object_url can only be set when status='approved' and object_url is currently NULL
  IF NEW.object_url IS DISTINCT FROM OLD.object_url AND NEW.object_url IS NOT NULL THEN
    IF OLD.status != 'approved' THEN
      RAISE EXCEPTION 'object_url can only be set when status is approved (current status: %)', OLD.status;
    END IF;
    IF OLD.object_url IS NOT NULL THEN
      RAISE EXCEPTION 'object_url can only be set once';
    END IF;
  END IF;

  -- Rule 2: build_id can only be set when object_url exists and build_id is currently NULL
  IF NEW.build_id IS DISTINCT FROM OLD.build_id AND NEW.build_id IS NOT NULL THEN
    IF NEW.object_url IS NULL THEN
      RAISE EXCEPTION 'build_id can only be set after object_url is set';
    END IF;
    IF OLD.build_id IS NOT NULL THEN
      RAISE EXCEPTION 'build_id can only be set once';
    END IF;
  END IF;

  -- Rule 3: release_id can only be set when build_id exists and release_id is currently NULL
  IF NEW.release_id IS DISTINCT FROM OLD.release_id AND NEW.release_id IS NOT NULL THEN
    IF NEW.build_id IS NULL THEN
      RAISE EXCEPTION 'release_id can only be set after build_id is set';
    END IF;
    IF OLD.release_id IS NOT NULL THEN
      RAISE EXCEPTION 'release_id can only be set once';
    END IF;
  END IF;

  -- Rule 4: status can only change to 'deployed' when release_id exists
  IF NEW.status IS DISTINCT FROM OLD.status AND NEW.status = 'deployed' THEN
    IF NEW.release_id IS NULL THEN
      RAISE EXCEPTION 'status can only be set to deployed after release_id is set';
    END IF;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enforce_deploy_approval_state_machine
  BEFORE UPDATE ON deploy_approval_requests
  FOR EACH ROW
  EXECUTE FUNCTION validate_deploy_approval_state_machine();

-- Indexes for deploy approval requests
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_public_id ON deploy_approval_requests(public_id);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_token ON deploy_approval_requests(target_api_token_id);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_status ON deploy_approval_requests(status);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_commit ON deploy_approval_requests(git_commit_hash);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_approved_at ON deploy_approval_requests(approved_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_updated_at ON deploy_approval_requests(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_object_url ON deploy_approval_requests(object_url) WHERE object_url IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_build ON deploy_approval_requests(build_id) WHERE build_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deploy_approval_requests_release ON deploy_approval_requests(release_id) WHERE release_id IS NOT NULL;

-- Ensure only one pending/approved request per commit+token combination
CREATE UNIQUE INDEX idx_deploy_approval_requests_active_commit
  ON deploy_approval_requests(git_commit_hash, target_api_token_id)
  WHERE status IN ('pending','approved');
