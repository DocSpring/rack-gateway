-- TOTP time-step consumption table for atomic replay protection
-- Each user can only use each time-step once (30-second window)
CREATE TABLE used_totp_steps (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  time_step BIGINT NOT NULL,
  method_id BIGINT REFERENCES mfa_methods(id) ON DELETE SET NULL,
  verified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip_address VARCHAR(45),
  user_agent TEXT,
  session_id BIGINT REFERENCES user_sessions(id) ON DELETE SET NULL,
  PRIMARY KEY (user_id, time_step)
);

-- Cleanup old time-steps (keep last 24 hours for audit)
CREATE INDEX idx_used_totp_steps_cleanup ON used_totp_steps(verified_at);

-- MFA attempts table for rate limiting and audit logging (supports TOTP and WebAuthn)
-- method_type: 1 = totp, 2 = webauthn
CREATE TABLE mfa_attempts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  method_id BIGINT REFERENCES mfa_methods(id) ON DELETE SET NULL,
  method_type SMALLINT NOT NULL CHECK (method_type IN (1, 2)),
  success BOOLEAN NOT NULL,
  attempted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip_address VARCHAR(45),
  user_agent TEXT,
  failure_reason TEXT,
  session_id BIGINT REFERENCES user_sessions(id) ON DELETE SET NULL
);

CREATE INDEX idx_mfa_attempts_rate_limit
  ON mfa_attempts(user_id, method_type, attempted_at DESC);

CREATE INDEX idx_mfa_attempts_failures
  ON mfa_attempts(user_id, attempted_at DESC)
  WHERE success = FALSE;
