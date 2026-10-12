-- A CLI login whose user needs no MFA challenge is marked MFA-verified without a factor. mfa_exempt records that, so
-- the CLI session it creates gets no step-up window: only a verified factor opens one.
ALTER TABLE cli_login_states ADD COLUMN IF NOT EXISTS mfa_exempt BOOLEAN NOT NULL DEFAULT FALSE;
