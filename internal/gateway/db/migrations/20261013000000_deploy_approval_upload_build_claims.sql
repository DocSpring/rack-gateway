-- An upload claims its approval's object_url before the gateway forwards it, and a build claims the approval's one
-- build, so two uploads (or builds) under one approval can't both reach the rack. object_uploaded_at records that
-- the claimed upload finished: a build may only use an archive whose upload finished.
ALTER TABLE deploy_approval_requests ADD COLUMN IF NOT EXISTS object_uploaded_at TIMESTAMPTZ;
ALTER TABLE deploy_approval_requests ADD COLUMN IF NOT EXISTS build_started_at TIMESTAMPTZ;

-- Until now object_url was only recorded once an upload had finished.
UPDATE deploy_approval_requests SET object_uploaded_at = updated_at
  WHERE object_url IS NOT NULL AND object_uploaded_at IS NULL;

-- An object holds at most one approval's archive. The rack overwrites an object key that's uploaded again, so the
-- gateway also refuses any upload to a key that is bound here.
DROP INDEX IF EXISTS idx_deploy_approval_requests_object_url;
CREATE UNIQUE INDEX idx_deploy_approval_requests_object_url
  ON deploy_approval_requests(object_url) WHERE object_url IS NOT NULL;
