DROP INDEX IF EXISTS idx_deploy_approval_requests_active;
CREATE UNIQUE INDEX IF NOT EXISTS idx_deploy_approval_requests_message_active
  ON deploy_approval_requests (message)
  WHERE status IN ('pending', 'approved');
