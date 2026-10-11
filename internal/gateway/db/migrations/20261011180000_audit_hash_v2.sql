-- ============================================================================
-- AUDIT HASH V2: EXPLICIT CHAIN INDEX, HASH EVERY STORED COLUMN
-- ============================================================================
--
-- Events written before this migration are hash version 1. Their HMAC can't be recomputed from the stored
-- row: it covered the timestamp in nanoseconds (Postgres keeps microseconds), the chain index the writer
-- computed (the stored one came from a sequence, which drifted after any failed insert), and some fields
-- before they were normalized for storage. Verification checks their previous_hash links only.
--
-- From now on the writer picks the id and the next chain index under the audit chain advisory lock, hashes
-- the row exactly as it will be stored (hash version 2) and passes every value to audit.append_audit_event,
-- which checks that the event follows the latest one.
-- ============================================================================

-- Existing rows are version 1. New rows must state their version, so the default is dropped right away
-- (rows that existed before keep 1).
ALTER TABLE audit.audit_event ADD COLUMN hash_version SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE audit.audit_event ALTER COLUMN hash_version DROP DEFAULT;
ALTER TABLE audit.audit_event ADD CONSTRAINT hash_version_known CHECK (hash_version IN (1, 2));

-- Where hash version 2 starts. Anchors record it, and verification compares the two.
CREATE INDEX idx_audit_event_hash_v2 ON audit.audit_event (chain_index) WHERE hash_version >= 2;

-- Chain indexes come from the writer now, not from a sequence or a default.
ALTER TABLE audit.audit_event ALTER COLUMN chain_index DROP DEFAULT;

DROP FUNCTION audit.append_audit_event(
  TIMESTAMPTZ, BYTEA, BYTEA, VARCHAR, BYTEA, VARCHAR, VARCHAR, BIGINT, VARCHAR, VARCHAR, VARCHAR, TEXT,
  VARCHAR, VARCHAR, TEXT, VARCHAR, INET, VARCHAR, VARCHAR, VARCHAR, INTEGER, INTEGER, INTEGER, BIGINT
);
DROP SEQUENCE audit.audit_event_chain_index_seq;

-- Replaced by `rack-gateway-api verify-audit`, which also recomputes hashes and checks the S3 anchors.
DROP FUNCTION audit.verify_chain(BIGINT, BIGINT);

-- The only way to insert an audit event. The caller holds the audit chain advisory lock, so the checks below
-- only fail for a buggy or hostile caller; they keep the chain contiguous and linked whatever it sends.
CREATE FUNCTION audit.append_audit_event(
  p_id BIGINT,
  p_chain_index BIGINT,
  p_timestamp TIMESTAMPTZ,
  p_previous_hash BYTEA,
  p_event_hash BYTEA,
  p_checkpoint_id VARCHAR,
  p_checkpoint_hash BYTEA,
  p_user_email VARCHAR,
  p_user_name VARCHAR,
  p_api_token_id BIGINT,
  p_api_token_name VARCHAR,
  p_action_type VARCHAR,
  p_action VARCHAR,
  p_command TEXT,
  p_resource VARCHAR,
  p_resource_type VARCHAR,
  p_details TEXT,
  p_request_id VARCHAR,
  p_ip_address INET,
  p_user_agent VARCHAR,
  p_status VARCHAR,
  p_rbac_decision VARCHAR,
  p_http_status INTEGER,
  p_response_time_ms INTEGER,
  p_event_count INTEGER,
  p_deploy_approval_request_id BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_latest_index BIGINT;
  v_latest_hash BYTEA;
BEGIN
  SELECT chain_index, event_hash INTO v_latest_index, v_latest_hash
  FROM audit.audit_event
  ORDER BY chain_index DESC
  LIMIT 1;

  IF p_chain_index IS DISTINCT FROM COALESCE(v_latest_index + 1, 0) THEN
    RAISE EXCEPTION 'audit event chain index % does not follow the latest index %', p_chain_index, v_latest_index;
  END IF;
  IF p_previous_hash IS DISTINCT FROM v_latest_hash THEN
    RAISE EXCEPTION 'audit event % previous_hash does not match the latest event', p_chain_index;
  END IF;

  INSERT INTO audit.audit_event (
    id, chain_index, hash_version, timestamp, previous_hash, event_hash, checkpoint_id, checkpoint_hash,
    user_email, user_name, api_token_id, api_token_name,
    action_type, action, command, resource, resource_type, details, request_id,
    ip_address, user_agent, status, rbac_decision, http_status, response_time_ms,
    event_count, deploy_approval_request_id
  ) VALUES (
    p_id, p_chain_index, 2, p_timestamp, p_previous_hash, p_event_hash, p_checkpoint_id, p_checkpoint_hash,
    p_user_email, p_user_name, p_api_token_id, p_api_token_name,
    p_action_type, p_action, p_command, p_resource, p_resource_type, p_details, p_request_id,
    p_ip_address, p_user_agent, p_status, p_rbac_decision, p_http_status, p_response_time_ms,
    p_event_count, p_deploy_approval_request_id
  );
END;
$$;

REVOKE ALL ON FUNCTION audit.append_audit_event FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit.append_audit_event TO audit_writer;

-- The writer takes each event id from the id sequence before hashing (the id is hashed).
GRANT USAGE ON SEQUENCE audit.audit_event_id_seq TO audit_writer;
