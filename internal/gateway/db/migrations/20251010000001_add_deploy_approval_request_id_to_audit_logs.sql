-- Add deploy_approval_request_id column to audit_logs table
-- to associate audit logs with deploy approval requests
ALTER TABLE audit_logs
  ADD COLUMN deploy_approval_request_id BIGINT REFERENCES deploy_approval_requests(id) ON DELETE SET NULL;

-- Create index for efficient lookups by deploy approval request
CREATE INDEX IF NOT EXISTS idx_audit_logs_deploy_approval_request_id
  ON audit_logs(deploy_approval_request_id, timestamp DESC);
