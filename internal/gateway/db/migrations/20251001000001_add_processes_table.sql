-- Track processes created via the gateway for authorization purposes
CREATE TABLE IF NOT EXISTS processes (
    id TEXT PRIMARY KEY,
    app TEXT NOT NULL,
    release_id TEXT,
    command TEXT,
    created_by_user_id BIGINT,
    created_by_api_token_id BIGINT,
    deploy_approval_request_id BIGINT REFERENCES deploy_approval_requests(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    terminated_at TIMESTAMP,

    -- Constraints
    CHECK (created_by_user_id IS NOT NULL OR created_by_api_token_id IS NOT NULL)
);

-- Index for looking up processes by creator
CREATE INDEX idx_processes_user ON processes(created_by_user_id) WHERE created_by_user_id IS NOT NULL;
CREATE INDEX idx_processes_token ON processes(created_by_api_token_id) WHERE created_by_api_token_id IS NOT NULL;
CREATE INDEX idx_processes_approval ON processes(deploy_approval_request_id) WHERE deploy_approval_request_id IS NOT NULL;
CREATE INDEX idx_processes_app ON processes(app);
