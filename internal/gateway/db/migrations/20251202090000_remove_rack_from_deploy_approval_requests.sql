-- +goose Up
-- Remove rack column from deploy_approval_requests (single-tenant design)
ALTER TABLE deploy_approval_requests DROP COLUMN IF EXISTS rack;

-- Drop the old unique index that included rack
DROP INDEX IF EXISTS idx_deploy_approval_requests_message_active;

-- Create new unique index without rack
CREATE UNIQUE INDEX idx_deploy_approval_requests_message_active
    ON deploy_approval_requests (message)
    WHERE status IN ('pending', 'approved');

-- +goose Down
-- Add back rack column
ALTER TABLE deploy_approval_requests ADD COLUMN rack VARCHAR(120) NOT NULL DEFAULT '';

-- Drop the new index
DROP INDEX IF EXISTS idx_deploy_approval_requests_message_active;

-- Recreate the old unique index with rack
CREATE UNIQUE INDEX idx_deploy_approval_requests_message_active
    ON deploy_approval_requests (rack, message)
    WHERE status IN ('pending', 'approved');
