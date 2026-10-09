-- CLI login now uses an RFC 8252 loopback redirect:
--   * the CLI sends an S256 code challenge, its own state and a 127.0.0.1 redirect URI to /auth/cli/start
--   * the identity provider code is exchanged at the callback (never stored), and only then is the
--     browser that completed the login bound to it with an HttpOnly cookie
--   * after MFA the browser is redirected to the CLI's loopback listener with a single-use login code
--   * /auth/cli/complete requires that login code AND the CLI's code verifier
-- Login states only live for minutes, so existing rows are discarded.
DELETE FROM cli_login_states;

ALTER TABLE cli_login_states RENAME COLUMN code_verifier TO oauth_code_verifier;

ALTER TABLE cli_login_states
  DROP COLUMN code,
  DROP COLUMN login_token,
  DROP COLUMN login_expires_at,
  ADD COLUMN cli_code_challenge VARCHAR(64) NOT NULL,
  ADD COLUMN cli_redirect_uri VARCHAR(64) NOT NULL,
  ADD COLUMN cli_state VARCHAR(128) NOT NULL,
  ADD COLUMN initiator_ip VARCHAR(64),
  ADD COLUMN initiator_device VARCHAR(64),
  ADD COLUMN browser_binding_hash VARCHAR(64),
  ADD COLUMN enrollment_required BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN login_code_hash VARCHAR(64) UNIQUE,
  ADD COLUMN login_code_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS cli_login_states_created_at_idx ON cli_login_states (created_at);
