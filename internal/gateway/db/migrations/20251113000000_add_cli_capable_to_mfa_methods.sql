-- Add cli_capable flag to mfa_methods for WebAuthn credentials
-- This allows users to mark which WebAuthn devices work with CLI vs browser-only (like 1Password)
ALTER TABLE mfa_methods ADD COLUMN IF NOT EXISTS cli_capable BOOLEAN NOT NULL DEFAULT FALSE;

-- Add index for filtering CLI-capable methods
CREATE INDEX IF NOT EXISTS idx_mfa_methods_cli_capable ON mfa_methods(cli_capable) WHERE type = 'webauthn';

COMMENT ON COLUMN mfa_methods.cli_capable IS 'Whether this MFA method can be used for CLI authentication (vs browser-only like 1Password)';
