ALTER TABLE cli_login_states
    ADD COLUMN IF NOT EXISTS login_error TEXT;
