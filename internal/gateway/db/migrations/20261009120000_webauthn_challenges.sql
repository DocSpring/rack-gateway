-- WebAuthn challenges live on the server. Clients only receive an opaque challenge ID, which is
-- bound to the user (and the session that started the ceremony), single-use and short-lived.
-- Previously the full session data (including the challenge) round-tripped through the client
-- and was trusted on the way back, so one captured assertion could be replayed indefinitely.
CREATE TABLE IF NOT EXISTS webauthn_challenges (
  id TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  session_id BIGINT REFERENCES user_sessions(id) ON DELETE CASCADE,
  purpose VARCHAR(20) NOT NULL CHECK (purpose IN ('assertion', 'registration')),
  session_data TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webauthn_challenges_expires ON webauthn_challenges(expires_at);
CREATE INDEX IF NOT EXISTS idx_webauthn_challenges_owner ON webauthn_challenges(user_id, session_id, purpose);

-- Stored signature counter for cloned-authenticator detection (WebAuthn §7.2 step 17).
ALTER TABLE mfa_methods ADD COLUMN IF NOT EXISTS webauthn_sign_count BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN mfa_methods.webauthn_sign_count IS 'Last WebAuthn signature counter seen for this credential';
