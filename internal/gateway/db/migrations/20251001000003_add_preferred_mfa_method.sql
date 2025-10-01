-- Add preferred_mfa_method column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferred_mfa_method VARCHAR(20);

-- Add check constraint to ensure valid method types
ALTER TABLE users ADD CONSTRAINT check_preferred_mfa_method
  CHECK (preferred_mfa_method IS NULL OR preferred_mfa_method IN ('totp', 'webauthn'));

-- Add comment
COMMENT ON COLUMN users.preferred_mfa_method IS 'User preferred MFA method for sign-in (totp or webauthn)';
