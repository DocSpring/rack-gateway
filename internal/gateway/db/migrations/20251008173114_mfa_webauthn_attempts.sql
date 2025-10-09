-- MFA WebAuthn attempts table for rate limiting and audit
CREATE TABLE mfa_webauthn_attempts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  method_id BIGINT REFERENCES mfa_methods(id) ON DELETE SET NULL,
  success BOOLEAN NOT NULL,
  attempted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip_address VARCHAR(45),
  user_agent TEXT,
  failure_reason TEXT,
  session_id BIGINT REFERENCES user_sessions(id) ON DELETE SET NULL
);

CREATE INDEX idx_mfa_webauthn_attempts_rate_limit
  ON mfa_webauthn_attempts(user_id, attempted_at DESC);

CREATE INDEX idx_mfa_webauthn_attempts_failures
  ON mfa_webauthn_attempts(user_id, attempted_at DESC)
  WHERE success = FALSE;
