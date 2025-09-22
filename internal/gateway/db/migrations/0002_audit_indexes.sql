CREATE INDEX IF NOT EXISTS idx_audit_logs_status ON audit_logs(status);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_type ON audit_logs(resource_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_timestamp ON audit_logs(user_email, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_status_action_resource_ts ON audit_logs(status, action_type, resource_type, timestamp DESC);
