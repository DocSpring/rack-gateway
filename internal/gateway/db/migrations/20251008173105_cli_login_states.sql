-- CLI login states for OAuth PKCE flow
CREATE TABLE IF NOT EXISTS cli_login_states (
  state VARCHAR(200) PRIMARY KEY,
  code VARCHAR(200),
  code_verifier TEXT,
  login_token TEXT,
  login_email VARCHAR(254),
  login_name VARCHAR(200),
  login_expires_at TIMESTAMPTZ,
  login_error TEXT,
  mfa_verified_at TIMESTAMPTZ,
  mfa_method_id BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
