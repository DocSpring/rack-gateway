ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS event_count INTEGER NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_audit_logs_user_event ON audit_logs(user_email, timestamp DESC);
