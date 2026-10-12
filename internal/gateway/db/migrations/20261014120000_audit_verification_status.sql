-- The outcome of the gateway's latest scheduled audit verification (the checks verify-audit runs), which the Audit
-- Logs page shows. One row. Verification itself doesn't read it: whoever can write to the database can change it.
CREATE TABLE IF NOT EXISTS audit_verification_status (
  id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
  -- The latest run.
  checked_at TIMESTAMPTZ NOT NULL,
  result VARCHAR(10) NOT NULL CHECK (result IN ('passed', 'failed', 'error')),
  failure_count INTEGER NOT NULL DEFAULT 0,
  warning_count INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL,
  -- The last passing run that checked every event.
  full_checked_at TIMESTAMPTZ,
  last_passed_at TIMESTAMPTZ,
  -- The first failing run since the last passing one (NULL while passing).
  failing_since TIMESTAMPTZ
);
