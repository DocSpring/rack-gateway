-- Migration: Fix audit log aggregation to only aggregate adjacent/consecutive events
-- Created: 2025-11-18
-- Description: The original trigger was incorrectly aggregating ALL matching events
-- instead of only ADJACENT events (where last_event_id = NEW.id - 1).
-- This migration also truncates the existing aggregated data since it was incorrectly
-- grouped (all matching events instead of only adjacent ones).

-- Drop ALL existing aggregation triggers and function
-- The base migration created t_maintain_aggregation, we're replacing it with audit_event_aggregation_trigger
DROP TRIGGER IF EXISTS t_maintain_aggregation ON audit.audit_event;
DROP TRIGGER IF EXISTS audit_event_aggregation_trigger ON audit.audit_event;
DROP FUNCTION IF EXISTS audit.aggregate_audit_event();

-- Clear the incorrectly aggregated data
-- The raw events in audit.audit_event remain untouched
TRUNCATE TABLE audit.audit_event_aggregated RESTART IDENTITY;

-- Create the new trigger function with adjacency check
CREATE FUNCTION audit.aggregate_audit_event()
RETURNS TRIGGER
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
  WHERE user_email = NEW.user_email
    AND COALESCE(user_name, '') = COALESCE(NEW.user_name, '')
    AND COALESCE(api_token_id, -1) = COALESCE(NEW.api_token_id, -1)
    AND COALESCE(api_token_name, '') = COALESCE(NEW.api_token_name, '')
    AND action_type = NEW.action_type
    AND action = NEW.action
    AND COALESCE(command, '') = COALESCE(NEW.command, '')
    AND COALESCE(resource, '') = COALESCE(NEW.resource, '')
    AND COALESCE(resource_type, '') = COALESCE(NEW.resource_type, '')
    AND COALESCE(details, '') = COALESCE(NEW.details, '')
    AND COALESCE(host(ip_address), '') = COALESCE(host(NEW.ip_address), '')
    AND COALESCE(user_agent, '') = COALESCE(NEW.user_agent, '')
    AND status = NEW.status
    AND COALESCE(rbac_decision, '') = COALESCE(NEW.rbac_decision, '')
    AND COALESCE(http_status, -1) = COALESCE(NEW.http_status, -1)
    AND COALESCE(deploy_approval_request_id, -1) = COALESCE(NEW.deploy_approval_request_id, -1)
    AND last_event_id = NEW.id - 1  -- ADJACENCY CHECK: only aggregate consecutive events
  LIMIT 1;

  IF v_agg_id IS NOT NULL THEN
    -- Update existing aggregated row
    UPDATE audit.audit_event_aggregated SET
      last_event_id = NEW.id,
      last_seen = NEW.timestamp,
      last_hash = NEW.event_hash,
      event_count = event_count + 1,
      min_response_time_ms = LEAST(min_response_time_ms, NEW.response_time_ms),
      max_response_time_ms = GREATEST(max_response_time_ms, NEW.response_time_ms),
      avg_response_time_ms = ((avg_response_time_ms * event_count) + NEW.response_time_ms) / (event_count + 1)
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
      NEW.id, NEW.id,
      NEW.timestamp, NEW.timestamp,
      NEW.event_hash, NEW.event_hash,
      1,
      NEW.response_time_ms, NEW.response_time_ms, NEW.response_time_ms,
      NEW.user_email, NEW.user_name, NEW.api_token_id, NEW.api_token_name,
      NEW.action_type, NEW.action,
      NEW.command, NEW.resource, NEW.resource_type, NEW.details,
      NEW.ip_address, NEW.user_agent,
      NEW.status, NEW.rbac_decision, NEW.http_status,
      NEW.deploy_approval_request_id
    );
  END IF;

  RETURN NEW;
END;
$$;

-- Recreate the trigger
CREATE TRIGGER audit_event_aggregation_trigger
AFTER INSERT ON audit.audit_event
FOR EACH ROW
EXECUTE FUNCTION audit.aggregate_audit_event();

-- Rebuild aggregated data from existing audit events with correct adjacency logic
-- Process all existing events in order and call the trigger function for each
DO $$
DECLARE
  event_record audit.audit_event%ROWTYPE;
BEGIN
  FOR event_record IN
    SELECT * FROM audit.audit_event ORDER BY id ASC
  LOOP
    PERFORM audit.aggregate_audit_event(event_record);
  END LOOP;

  RAISE NOTICE 'Successfully rebuilt % aggregated audit log entries', (SELECT COUNT(*) FROM audit.audit_event_aggregated);
END $$;
