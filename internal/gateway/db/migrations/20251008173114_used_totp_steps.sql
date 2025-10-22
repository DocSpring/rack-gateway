-- TOTP time-step consumption table for atomic replay protection
-- Each user can only use each time-step once (30-second window)
CREATE TABLE used_totp_steps (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  time_step BIGINT NOT NULL,
  method_id BIGINT REFERENCES mfa_methods(id) ON DELETE SET NULL,
  verified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip_address VARCHAR(45),
  user_agent TEXT,
  session_id BIGINT REFERENCES user_sessions(id) ON DELETE SET NULL,
  PRIMARY KEY (user_id, time_step)
);

-- Cleanup old time-steps (keep last 24 hours for audit)
CREATE INDEX idx_used_totp_steps_cleanup ON used_totp_steps(verified_at);
