-- Adjust deploy approval request uniqueness to allow only one pending/approved request per rack/message combination
DROP INDEX IF EXISTS idx_deploy_approval_requests_active;
CREATE UNIQUE INDEX IF NOT EXISTS idx_deploy_approval_requests_message_active
  ON deploy_approval_requests (rack, message)
  WHERE status IN ('pending', 'approved');
