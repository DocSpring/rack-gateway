-- Add account locking fields to users table
ALTER TABLE users
    ADD COLUMN locked_at TIMESTAMP,
    ADD COLUMN locked_reason TEXT,
    ADD COLUMN locked_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN unlocked_at TIMESTAMP,
    ADD COLUMN unlocked_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_users_locked ON users(locked_at) WHERE locked_at IS NOT NULL;

-- Add mfa_totp_attempts table for replay protection, rate limiting, and audit
CREATE TABLE mfa_totp_attempts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method_id BIGINT REFERENCES mfa_methods(id) ON DELETE SET NULL,
    code_hash VARCHAR(64) NOT NULL,
    success BOOLEAN NOT NULL,
    attempted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ip_address VARCHAR(45),
    user_agent TEXT,
    failure_reason TEXT,
    session_id BIGINT REFERENCES user_sessions(id) ON DELETE SET NULL
);

-- Replay detection (recent successful attempts)
CREATE INDEX idx_mfa_totp_attempts_replay
    ON mfa_totp_attempts(user_id, code_hash, attempted_at DESC)
    WHERE success = TRUE;

-- Rate limiting and analytics (recent attempts by user)
CREATE INDEX idx_mfa_totp_attempts_rate_limit
    ON mfa_totp_attempts(user_id, attempted_at DESC);

CREATE INDEX idx_mfa_totp_attempts_failures
    ON mfa_totp_attempts(user_id, attempted_at DESC)
    WHERE success = FALSE;

-- Add mfa_webauthn_attempts table for WebAuthn rate limiting and audit
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

-- Rate limiting and analytics (recent attempts by user)
CREATE INDEX idx_mfa_webauthn_attempts_rate_limit
    ON mfa_webauthn_attempts(user_id, attempted_at DESC);

CREATE INDEX idx_mfa_webauthn_attempts_failures
    ON mfa_webauthn_attempts(user_id, attempted_at DESC)
    WHERE success = FALSE;
