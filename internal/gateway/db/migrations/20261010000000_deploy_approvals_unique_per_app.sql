-- CI deploys several apps for one commit with the same CI token (e.g. docspring and api-proxy), so an open
-- request is unique per commit, target token AND app. Before this, the second app's request collided with the
-- first one's.
DROP INDEX IF EXISTS idx_deploy_approval_requests_active_commit;
CREATE UNIQUE INDEX idx_deploy_approval_requests_active_commit
  ON deploy_approval_requests(git_commit_hash, target_api_token_id, app)
  WHERE status IN ('pending','approved');

-- Approvals whose window has passed are expired, so they no longer count as open (and block new requests).
UPDATE deploy_approval_requests
  SET status = 'expired', updated_at = NOW()
  WHERE status = 'approved' AND approval_expires_at IS NOT NULL AND approval_expires_at <= NOW();
