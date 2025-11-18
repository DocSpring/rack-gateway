-- Migration: Fix audit log aggregation to only aggregate adjacent/consecutive events
-- Created: 2025-11-18
-- Description: The original trigger was incorrectly aggregating ALL matching events
-- instead of only ADJACENT events (where last_event_id = NEW.id - 1).
-- This migration also truncates the existing aggregated data since it was incorrectly
-- grouped (all matching events instead of only adjacent ones).

-- Drop ALL existing aggregation triggers and functions
-- The base migration created t_maintain_aggregation + audit.maintain_aggregation()
-- We're replacing with audit_event_aggregation_trigger + new functions
DROP TRIGGER IF EXISTS t_maintain_aggregation ON audit.audit_event;
DROP TRIGGER IF EXISTS audit_event_aggregation_trigger ON audit.audit_event;
DROP FUNCTION IF EXISTS audit.maintain_aggregation();
DROP FUNCTION IF EXISTS audit.aggregate_audit_event();
DROP FUNCTION IF EXISTS audit.process_event_for_aggregation(audit.audit_event);

-- Clear the incorrectly aggregated data
-- The raw events in audit.audit_event remain untouched
TRUNCATE TABLE audit.audit_event_aggregated RESTART IDENTITY;

-- Create shared aggregation logic function that accepts an event row
CREATE FUNCTION audit.process_event_for_aggregation(event_row audit.audit_event)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
  v_agg_id INTEGER;
BEGIN
  -- Try to find existing aggregated row matching all grouping fields
  -- AND the most recent event (last_event_id) is exactly one less than the new event
  SELECT id INTO v_agg_id
  FROM audit.audit_event_aggregated
  WHERE user_email = event_row.user_email
    AND COALESCE(user_name, '') = COALESCE(event_row.user_name, '')
    AND COALESCE(api_token_id, -1) = COALESCE(event_row.api_token_id, -1)
    AND COALESCE(api_token_name, '') = COALESCE(event_row.api_token_name, '')
    AND action_type = event_row.action_type
    AND action = event_row.action
    AND COALESCE(command, '') = COALESCE(event_row.command, '')
    AND COALESCE(resource, '') = COALESCE(event_row.resource, '')
    AND COALESCE(resource_type, '') = COALESCE(event_row.resource_type, '')
    AND COALESCE(details, '') = COALESCE(event_row.details, '')
    AND COALESCE(host(ip_address), '') = COALESCE(host(event_row.ip_address), '')
    AND COALESCE(user_agent, '') = COALESCE(event_row.user_agent, '')
    AND status = event_row.status
    AND COALESCE(rbac_decision, '') = COALESCE(event_row.rbac_decision, '')
    AND COALESCE(http_status, -1) = COALESCE(event_row.http_status, -1)
    AND COALESCE(deploy_approval_request_id, -1) = COALESCE(event_row.deploy_approval_request_id, -1)
    AND last_event_id = event_row.id - 1  -- ADJACENCY CHECK: only aggregate consecutive events
  LIMIT 1;

  IF v_agg_id IS NOT NULL THEN
    -- Update existing aggregated row
    UPDATE audit.audit_event_aggregated SET
      last_event_id = event_row.id,
      last_seen = event_row.timestamp,
      last_hash = event_row.event_hash,
      event_count = event_count + 1,
      min_response_time_ms = LEAST(min_response_time_ms, event_row.response_time_ms),
      max_response_time_ms = GREATEST(max_response_time_ms, event_row.response_time_ms),
      avg_response_time_ms = ((avg_response_time_ms * event_count) + event_row.response_time_ms) / (event_count + 1)
    WHERE id = v_agg_id;
  ELSE
    -- Insert new aggregated row
    INSERT INTO audit.audit_event_aggregated (
      first_event_id, last_event_id,
      first_seen, last_seen,
      first_hash, last_hash,
      event_count,
      min_response_time_ms, max_response_time_ms, avg_response_time_ms,
      user_email, user_name, api_token_id, api_token_name,
      action_type, action,
      command, resource, resource_type, details,
      ip_address, user_agent,
      status, rbac_decision, http_status,
      deploy_approval_request_id
    ) VALUES (
      event_row.id, event_row.id,
      event_row.timestamp, event_row.timestamp,
      event_row.event_hash, event_row.event_hash,
      1,
      event_row.response_time_ms, event_row.response_time_ms, event_row.response_time_ms,
      event_row.user_email, event_row.user_name, event_row.api_token_id, event_row.api_token_name,
      event_row.action_type, event_row.action,
      event_row.command, event_row.resource, event_row.resource_type, event_row.details,
      event_row.ip_address, event_row.user_agent,
      event_row.status, event_row.rbac_decision, event_row.http_status,
      event_row.deploy_approval_request_id
    );
  END IF;
END;
$$;

-- Create the trigger function that calls the shared logic
CREATE FUNCTION audit.aggregate_audit_event()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
  PERFORM audit.process_event_for_aggregation(NEW);
  RETURN NEW;
END;
$$;

-- Recreate the trigger
CREATE TRIGGER audit_event_aggregation_trigger
AFTER INSERT ON audit.audit_event
FOR EACH ROW
EXECUTE FUNCTION audit.aggregate_audit_event();

-- Rebuild aggregated data from existing audit events with correct adjacency logic
-- CRITICAL: We NEVER modify audit.audit_event - it's immutable
DO $$
DECLARE
  current_event audit.audit_event%ROWTYPE;
BEGIN
  -- Process all events in order using the shared aggregation function
  FOR current_event IN
    SELECT * FROM audit.audit_event ORDER BY id ASC
  LOOP
    PERFORM audit.process_event_for_aggregation(current_event);
  END LOOP;

  RAISE NOTICE 'Successfully rebuilt % aggregated audit log entries from % events',
    (SELECT COUNT(*) FROM audit.audit_event_aggregated),
    (SELECT COUNT(*) FROM audit.audit_event);
END $$;
