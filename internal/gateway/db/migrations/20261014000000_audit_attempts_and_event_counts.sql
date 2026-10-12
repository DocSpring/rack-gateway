-- ============================================================================
-- AUDIT ATTEMPTS AND THROTTLED DENIALS IN THE AGGREGATED VIEW
-- ============================================================================
--
-- State-changing requests now write an "attempted" entry before they act, and their result entries after,
-- all with the request's ID (which the gateway generates for every request). audit.audit_event keeps every
-- entry. The aggregated table the web UI lists shows a request's attempt until its first result arrives, and
-- that result in its place from then on (first_event_id still points at the attempt).
--
-- Repeated denials are written as one entry whose event_count says how many denials it stands for, so the
-- aggregated table now adds up event_count instead of counting rows.
--
-- Only the aggregation changes: audit.audit_event, its hash chain and append_audit_event are untouched.
-- ============================================================================

-- Finds the aggregated row an attempt's result takes over (and the adjacency check's previous row).
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_last_event_id
  ON audit.audit_event_aggregated (last_event_id);

CREATE OR REPLACE FUNCTION audit.process_event_for_aggregation(event_row audit.audit_event)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
  v_agg_id INTEGER;
BEGIN
  -- A request's first result takes over the row of the request's attempt.
  IF event_row.status <> 'attempted' AND event_row.request_id IS NOT NULL THEN
    SELECT agg.id INTO v_agg_id
    FROM audit.audit_event attempt
    JOIN audit.audit_event_aggregated agg ON agg.last_event_id = attempt.id
    WHERE attempt.request_id = event_row.request_id
      AND attempt.status = 'attempted'
      AND attempt.user_email = event_row.user_email
      AND attempt.id < event_row.id
    ORDER BY attempt.id DESC
    LIMIT 1;

    IF v_agg_id IS NOT NULL THEN
      UPDATE audit.audit_event_aggregated SET
        last_event_id = event_row.id,
        last_seen = event_row.timestamp,
        last_hash = event_row.event_hash,
        event_count = event_row.event_count,
        min_response_time_ms = event_row.response_time_ms,
        max_response_time_ms = event_row.response_time_ms,
        avg_response_time_ms = event_row.response_time_ms,
        user_name = event_row.user_name,
        api_token_id = event_row.api_token_id,
        api_token_name = event_row.api_token_name,
        action_type = event_row.action_type,
        action = event_row.action,
        command = event_row.command,
        resource = event_row.resource,
        resource_type = event_row.resource_type,
        details = event_row.details,
        ip_address = event_row.ip_address,
        user_agent = event_row.user_agent,
        status = event_row.status,
        rbac_decision = event_row.rbac_decision,
        http_status = event_row.http_status,
        deploy_approval_request_id = event_row.deploy_approval_request_id
      WHERE id = v_agg_id;

      RETURN;
    END IF;
  END IF;

  -- Merge process.exec into process.exec.start (unchanged; an attempt is never merged here).
  IF event_row.action = 'process.exec' AND event_row.status <> 'attempted' THEN
    SELECT id INTO v_agg_id
    FROM audit.audit_event_aggregated
    WHERE user_email = event_row.user_email
      AND action = 'process.exec.start'
      AND COALESCE(resource, '') = COALESCE(event_row.resource, '')
      AND COALESCE(command, '') = COALESCE(event_row.command, '')
      AND COALESCE(host(ip_address), '') = COALESCE(host(event_row.ip_address), '')
      AND COALESCE(user_agent, '') = COALESCE(event_row.user_agent, '')
    ORDER BY last_seen DESC
    LIMIT 1;

    IF v_agg_id IS NOT NULL THEN
      UPDATE audit.audit_event_aggregated SET
        action = 'process.exec',
        last_event_id = event_row.id,
        last_seen = event_row.timestamp,
        last_hash = event_row.event_hash,
        min_response_time_ms = event_row.response_time_ms,
        max_response_time_ms = event_row.response_time_ms,
        avg_response_time_ms = event_row.response_time_ms,
        event_count = 1,
        status = event_row.status
      WHERE id = v_agg_id;

      RETURN;
    END IF;
  END IF;

  -- Identical consecutive events share a row. An attempt always gets a row of its own, for its result to take
  -- over.
  IF event_row.status <> 'attempted' THEN
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
      AND last_event_id = event_row.id - 1
    LIMIT 1;
  END IF;

  IF v_agg_id IS NOT NULL THEN
    UPDATE audit.audit_event_aggregated SET
      last_event_id = event_row.id,
      last_seen = event_row.timestamp,
      last_hash = event_row.event_hash,
      event_count = event_count + event_row.event_count,
      min_response_time_ms = LEAST(min_response_time_ms, event_row.response_time_ms),
      max_response_time_ms = GREATEST(max_response_time_ms, event_row.response_time_ms),
      -- Weighted by event_count. BIGINT, so the products can't overflow (an error here would fail the insert).
      avg_response_time_ms = (
        avg_response_time_ms::BIGINT * event_count + event_row.response_time_ms::BIGINT * event_row.event_count
      ) / (event_count + event_row.event_count)
    WHERE id = v_agg_id;
  ELSE
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
      event_row.event_count,
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
