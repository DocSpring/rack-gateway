-- A promote claims its approval before the gateway forwards it, so two concurrent promotes with one approval can't
-- both reach the rack. A failed promote clears the claim so it can be retried.
ALTER TABLE deploy_approval_requests ADD COLUMN IF NOT EXISTS promote_started_at TIMESTAMPTZ;
