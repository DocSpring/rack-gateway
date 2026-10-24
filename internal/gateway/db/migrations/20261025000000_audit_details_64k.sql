-- An audit entry's details may now be up to 64,000 characters (they were 8,000), so an exec's command can be kept in
-- full up to the gateway's 50 KB command limit. The previous release only writes details that fit the old limit.
ALTER TABLE audit.audit_event
  DROP CONSTRAINT audit_event_details_check,
  ADD CONSTRAINT audit_event_details_check CHECK (char_length(details) <= 64000);
