-- Adjust deploy request uniqueness to allow only one pending/approved request per rack/message combination
DROP INDEX IF EXISTS idx_deploy_requests_active;
CREATE UNIQUE INDEX IF NOT EXISTS idx_deploy_requests_message_active
  ON deploy_requests (rack, message)
  WHERE status IN ('pending', 'approved');
