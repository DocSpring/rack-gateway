-- Users table with MFA support
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  email VARCHAR(254) NOT NULL UNIQUE,
  name VARCHAR(120) NOT NULL,
  roles TEXT NOT NULL CHECK (char_length(roles) <= 1024),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  suspended BOOLEAN NOT NULL DEFAULT FALSE,
  mfa_enrolled BOOLEAN NOT NULL DEFAULT FALSE,
  mfa_enforced_at TIMESTAMPTZ,
  preferred_mfa_method VARCHAR(20),
  locked_at TIMESTAMP,
  locked_reason TEXT,
  locked_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT check_preferred_mfa_method CHECK (preferred_mfa_method IS NULL OR preferred_mfa_method IN ('totp', 'webauthn'))
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX idx_users_locked ON users(locked_at) WHERE locked_at IS NOT NULL;

COMMENT ON COLUMN users.preferred_mfa_method IS 'User preferred MFA method for sign-in (totp or webauthn)';
