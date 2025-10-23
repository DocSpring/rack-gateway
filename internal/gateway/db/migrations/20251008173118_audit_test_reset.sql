-- Helper function allowing automated tests to reset audit tables when explicitly enabled.
CREATE OR REPLACE FUNCTION audit.reset_for_tests()
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
  IF COALESCE(lower(current_setting('audit.allow_reset', true)), 'off') NOT IN ('on', 'true', '1') THEN
    RAISE EXCEPTION 'audit.reset_for_tests() may only be called when audit.allow_reset is on';
  END IF;

  TRUNCATE TABLE audit.audit_event_aggregated RESTART IDENTITY CASCADE;
  TRUNCATE TABLE audit.audit_event CASCADE;
END;
$$;

REVOKE ALL ON FUNCTION audit.reset_for_tests() FROM PUBLIC;
