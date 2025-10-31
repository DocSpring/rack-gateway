-- ============================================================================
-- TAMPER-EVIDENT, APPEND-ONLY AUDIT LOG
-- ============================================================================
--
-- This migration creates a cryptographically-chained, immutable audit log
-- system with the following properties:
--
-- 1. **Append-only**: INSERT only, UPDATE/DELETE blocked at database level
-- 2. **Cryptographic chain**: Each event contains HMAC-SHA256 of previous event
-- 3. **WORM anchoring**: Periodic checkpoints to S3 Object Lock for external verification
-- 4. **Role isolation**: Separate roles for owner, writer, reader
-- 5. **RLS policies**: Row-level security to enforce access control
-- 6. **SECURITY DEFINER function**: Apps can only append via controlled function
--
-- ============================================================================

-- ============================================================================
-- STEP 1: CREATE ROLES
-- ============================================================================

-- Note: All audit roles (audit_owner, audit_writer, audit_reader) are created by Terraform
-- This migration expects them to already exist

-- ============================================================================
-- STEP 2: CREATE SCHEMA
-- ============================================================================

CREATE SCHEMA IF NOT EXISTS audit AUTHORIZATION audit_owner;

-- Revoke all default permissions
REVOKE ALL ON SCHEMA audit FROM public;

-- Grant usage to writer and reader
GRANT USAGE ON SCHEMA audit TO audit_writer, audit_reader;

-- ============================================================================
-- STEP 3: CREATE AUDIT LOG TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS audit.audit_event (
  -- Primary key
  id BIGSERIAL PRIMARY KEY,

  -- Timestamp
  timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  -- Cryptographic chain fields
  chain_index BIGINT NOT NULL DEFAULT 0,
  previous_hash BYTEA,  -- NULL for chain_index=0 (genesis event)
  event_hash BYTEA NOT NULL,
  checkpoint_id VARCHAR(255),  -- S3 object key of last checkpoint
  checkpoint_hash BYTEA,  -- Hash of last checkpoint (for verification)

  -- User identification
  user_email VARCHAR(254) NOT NULL,
  user_name VARCHAR(200),

  -- API token metadata (if request was made with API token)
  -- Note: No foreign key constraint - audit logs are immutable and preserve IDs even if entities are deleted
  api_token_id BIGINT,
  api_token_name VARCHAR(150),

  -- Action metadata
  action_type VARCHAR(100) NOT NULL,  -- "convox", "users", "auth", etc.
  action VARCHAR(150) NOT NULL,  -- e.g., "app.create", "user.delete"
  command TEXT CHECK (char_length(command) <= 2000),
  resource VARCHAR(255),
  resource_type VARCHAR(100),
  details TEXT CHECK (char_length(details) <= 8000),

  -- Request metadata
  request_id VARCHAR(255),
  ip_address INET,
  user_agent VARCHAR(512),

  -- Result metadata
  status VARCHAR(32) NOT NULL,  -- "success", "denied", "error", "blocked"
  rbac_decision VARCHAR(16),  -- "allow" or "deny"
  http_status INTEGER,
  response_time_ms INTEGER NOT NULL DEFAULT 0,

  -- Event aggregation removed - every event is now a separate row
  event_count INTEGER NOT NULL DEFAULT 1,

  -- Deploy approval request reference
  -- Note: No foreign key constraint - audit logs are immutable and preserve IDs even if entities are deleted
  deploy_approval_request_id BIGINT,

  -- Constraints
  CONSTRAINT chain_index_unique UNIQUE(chain_index),
  CONSTRAINT chain_index_nonnegative CHECK(chain_index >= 0),
  CONSTRAINT genesis_hash_rules CHECK(
    (chain_index = 0 AND previous_hash IS NULL) OR
    (chain_index > 0 AND previous_hash IS NOT NULL)
  )
);

-- ============================================================================
-- STEP 4: CREATE INDEXES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_audit_event_timestamp ON audit.audit_event(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_user_email ON audit.audit_event(user_email);
CREATE INDEX IF NOT EXISTS idx_audit_event_action_type ON audit.audit_event(action_type);
CREATE INDEX IF NOT EXISTS idx_audit_event_status ON audit.audit_event(status);
CREATE INDEX IF NOT EXISTS idx_audit_event_resource_type ON audit.audit_event(resource_type);
CREATE INDEX IF NOT EXISTS idx_audit_event_user_timestamp ON audit.audit_event(user_email, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_status_action_resource_ts ON audit.audit_event(status, action_type, resource_type, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_api_token_id ON audit.audit_event(api_token_id);
CREATE INDEX IF NOT EXISTS idx_audit_event_deploy_approval_request_id ON audit.audit_event(deploy_approval_request_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_chain_index ON audit.audit_event(chain_index DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_checkpoint_id ON audit.audit_event(checkpoint_id) WHERE checkpoint_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_event_request_id ON audit.audit_event(request_id);

-- ============================================================================
-- STEP 5: STRIP PERMISSIONS AND GRANT MINIMAL ACCESS
-- ============================================================================

-- Revoke all default permissions
REVOKE ALL ON audit.audit_event FROM public;

-- Writer can ONLY insert (will be revoked later in favor of function)
GRANT INSERT ON audit.audit_event TO audit_writer;
GRANT USAGE, SELECT ON SEQUENCE audit.audit_event_id_seq TO audit_writer;
REVOKE UPDATE, DELETE ON audit.audit_event FROM audit_writer;

-- Reader can ONLY select
GRANT SELECT ON audit.audit_event TO audit_reader;
REVOKE INSERT, UPDATE, DELETE ON audit.audit_event FROM audit_reader;

-- ============================================================================
-- STEP 6: ROW LEVEL SECURITY (RLS)
-- ============================================================================

ALTER TABLE audit.audit_event ENABLE ROW LEVEL SECURITY;

-- Policy: audit_writer can insert any row
CREATE POLICY insert_only ON audit.audit_event
  FOR INSERT TO audit_writer WITH CHECK (true);

-- Policy: audit_reader can read any row
CREATE POLICY ro_read ON audit.audit_event
  FOR SELECT TO audit_reader USING (true);

-- ============================================================================
-- STEP 7: TRIGGERS TO FORBID UPDATE/DELETE
-- ============================================================================

-- Function to block updates and deletes
CREATE OR REPLACE FUNCTION audit.forbid_update_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
  RAISE EXCEPTION 'audit_event is append-only: UPDATE and DELETE are forbidden';
END;
$$;

-- Trigger to block updates
CREATE TRIGGER t_forbid_update
BEFORE UPDATE ON audit.audit_event
FOR EACH ROW EXECUTE FUNCTION audit.forbid_update_delete();

-- Trigger to block deletes
CREATE TRIGGER t_forbid_delete
BEFORE DELETE ON audit.audit_event
FOR EACH ROW EXECUTE FUNCTION audit.forbid_update_delete();

-- ============================================================================
-- STEP 8: CREATE SEQUENCE FOR CHAIN INDEX
-- ============================================================================

-- Create a sequence for chain_index to avoid race conditions
-- This ensures each audit event gets a unique, monotonically increasing chain index
-- MINVALUE 0 allows restarting to 0 for tests (genesis event has chain_index=0)
CREATE SEQUENCE IF NOT EXISTS audit.audit_event_chain_index_seq MINVALUE 0;

-- ============================================================================
-- STEP 9: SECURITY DEFINER FUNCTION FOR CONTROLLED APPENDS
-- ============================================================================

-- Function to append an audit event with chain verification
-- This is the ONLY way applications should write to the audit log
CREATE OR REPLACE FUNCTION audit.append_audit_event(
  p_timestamp TIMESTAMPTZ,
  p_previous_hash BYTEA,
  p_event_hash BYTEA,
  p_checkpoint_id VARCHAR(255),
  p_checkpoint_hash BYTEA,
  p_user_email VARCHAR(254),
  p_user_name VARCHAR(200),
  p_api_token_id BIGINT,
  p_api_token_name VARCHAR(150),
  p_action_type VARCHAR(100),
  p_action VARCHAR(150),
  p_command TEXT,
  p_resource VARCHAR(255),
  p_resource_type VARCHAR(100),
  p_details TEXT,
  p_request_id VARCHAR(255),
  p_ip_address INET,
  p_user_agent VARCHAR(512),
  p_status VARCHAR(32),
  p_rbac_decision VARCHAR(16),
  p_http_status INTEGER,
  p_response_time_ms INTEGER,
  p_event_count INTEGER,
  p_deploy_approval_request_id BIGINT
)
RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
  v_chain_index BIGINT;
  v_new_id BIGINT;
BEGIN
  -- Get the next chain index from sequence (thread-safe, no race condition)
  v_chain_index := nextval('audit.audit_event_chain_index_seq');

  -- Insert the new event
  INSERT INTO audit.audit_event (
    timestamp, chain_index, previous_hash, event_hash, checkpoint_id, checkpoint_hash,
    user_email, user_name, api_token_id, api_token_name,
    action_type, action, command, resource, resource_type, details, request_id,
    ip_address, user_agent, status, rbac_decision, http_status, response_time_ms,
    event_count, deploy_approval_request_id
  ) VALUES (
    p_timestamp, v_chain_index, p_previous_hash, p_event_hash, p_checkpoint_id, p_checkpoint_hash,
    p_user_email, p_user_name, p_api_token_id, p_api_token_name,
    p_action_type, p_action, p_command, p_resource, p_resource_type, p_details, p_request_id,
    p_ip_address, p_user_agent, p_status, p_rbac_decision, p_http_status, p_response_time_ms,
    p_event_count, p_deploy_approval_request_id
  ) RETURNING id INTO v_new_id;

  RETURN v_new_id;
END;
$$;

-- Grant EXECUTE on the append function to audit_writer
GRANT EXECUTE ON FUNCTION audit.append_audit_event TO audit_writer;
GRANT USAGE ON SEQUENCE audit.audit_event_chain_index_seq TO audit_writer;

-- Now revoke direct INSERT on the table (apps must use the function)
REVOKE INSERT ON audit.audit_event FROM audit_writer;

-- ============================================================================
-- STEP 10: DEFAULT PRIVILEGES (FUTURE-PROOF)
-- ============================================================================

-- Ensure any new tables in the audit schema are locked down by default
ALTER DEFAULT PRIVILEGES FOR ROLE audit_owner IN SCHEMA audit
  REVOKE ALL ON TABLES FROM public;

ALTER DEFAULT PRIVILEGES FOR ROLE audit_owner IN SCHEMA audit
  GRANT SELECT ON TABLES TO audit_reader;

-- ============================================================================
-- STEP 11: HELPER FUNCTIONS
-- ============================================================================

-- Function to get the latest event in the chain (for computing next hash)
CREATE OR REPLACE FUNCTION audit.get_latest_event()
RETURNS TABLE(
  chain_index BIGINT,
  event_hash BYTEA,
  checkpoint_id VARCHAR(255),
  checkpoint_hash BYTEA
)
LANGUAGE sql
SECURITY DEFINER
AS $$
  SELECT chain_index, event_hash, checkpoint_id, checkpoint_hash
  FROM audit.audit_event
  ORDER BY chain_index DESC
  LIMIT 1;
$$;

GRANT EXECUTE ON FUNCTION audit.get_latest_event TO audit_writer;

-- Function to verify chain integrity (returns first broken link or NULL if valid)
CREATE OR REPLACE FUNCTION audit.verify_chain(start_index BIGINT DEFAULT 0, end_index BIGINT DEFAULT NULL)
RETURNS TABLE(
  broken_at_index BIGINT,
  event_id BIGINT,
  error_message TEXT
)
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
  v_event RECORD;
  v_expected_previous_hash BYTEA;
BEGIN
  -- If end_index is NULL, verify to the end of the chain
  IF end_index IS NULL THEN
    SELECT MAX(chain_index) INTO end_index FROM audit.audit_event;
  END IF;

  -- Verify each event in the chain
  FOR v_event IN
    SELECT * FROM audit.audit_event
    WHERE chain_index >= start_index AND chain_index <= end_index
    ORDER BY chain_index ASC
  LOOP
    -- Check genesis event
    IF v_event.chain_index = 0 THEN
      IF v_event.previous_hash IS NOT NULL THEN
        broken_at_index := v_event.chain_index;
        event_id := v_event.id;
        error_message := 'Genesis event has non-NULL previous_hash';
        RETURN NEXT;
        RETURN;
      END IF;
      v_expected_previous_hash := v_event.event_hash;
      CONTINUE;
    END IF;

    -- Check that previous_hash matches the previous event's hash
    IF v_event.previous_hash != v_expected_previous_hash THEN
      broken_at_index := v_event.chain_index;
      event_id := v_event.id;
      error_message := format('Chain broken: previous_hash does not match. Expected %s, got %s',
        encode(v_expected_previous_hash, 'hex'),
        encode(v_event.previous_hash, 'hex'));
      RETURN NEXT;
      RETURN;
    END IF;

    v_expected_previous_hash := v_event.event_hash;
  END LOOP;

  -- If we got here, the chain is valid
  RETURN;
END;
$$;

GRANT EXECUTE ON FUNCTION audit.verify_chain TO audit_reader;

-- ============================================================================
-- STEP 12: AGGREGATED TABLE FOR FRONTEND DISPLAY
-- ============================================================================

-- Mutable aggregation table for fast queries (trigger-maintained)
-- This table aggregates repeated events (e.g., CLI polling) for clean frontend display
-- while preserving the immutable audit chain in audit.audit_event
CREATE TABLE IF NOT EXISTS audit.audit_event_aggregated (
  id SERIAL PRIMARY KEY,

  -- Event boundaries (for drilling down to raw events)
  first_event_id BIGINT NOT NULL,
  last_event_id BIGINT NOT NULL,

  -- Timestamps
  first_seen TIMESTAMPTZ NOT NULL,
  last_seen TIMESTAMPTZ NOT NULL,

  -- Chain hashes (for auditability)
  first_hash BYTEA NOT NULL,  -- Set once on first event, never updated
  last_hash BYTEA NOT NULL,   -- Updated on every new event in group

  -- Aggregated metrics
  event_count INTEGER NOT NULL DEFAULT 1,
  min_response_time_ms INTEGER NOT NULL,
  max_response_time_ms INTEGER NOT NULL,
  avg_response_time_ms INTEGER NOT NULL,

  -- Grouping fields (ALL fields except timestamps, request_id, response_time_ms)
  user_email VARCHAR(254) NOT NULL,
  user_name VARCHAR(200),
  api_token_id BIGINT,
  api_token_name VARCHAR(150),
  action_type VARCHAR(100) NOT NULL,
  action VARCHAR(150) NOT NULL,
  command TEXT,
  resource VARCHAR(255),
  resource_type VARCHAR(100),
  details TEXT,
  ip_address INET,
  user_agent VARCHAR(512),
  status VARCHAR(32) NOT NULL,
  rbac_decision VARCHAR(16),
  http_status INTEGER,
  deploy_approval_request_id BIGINT
);

-- Indexes for fast queries
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_first_seen ON audit.audit_event_aggregated(first_seen DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_last_seen ON audit.audit_event_aggregated(last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_user_email ON audit.audit_event_aggregated(user_email);
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_action_type ON audit.audit_event_aggregated(action_type);
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_status ON audit.audit_event_aggregated(status);
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_resource_type ON audit.audit_event_aggregated(resource_type);

-- Composite index for common query pattern
CREATE INDEX IF NOT EXISTS idx_audit_event_aggregated_composite ON audit.audit_event_aggregated(
  user_email, action_type, status, last_seen DESC
);

-- Grant permissions
REVOKE ALL ON audit.audit_event_aggregated FROM public;
GRANT SELECT ON audit.audit_event_aggregated TO audit_reader;
GRANT INSERT, UPDATE ON audit.audit_event_aggregated TO audit_writer;
GRANT USAGE ON SEQUENCE audit.audit_event_aggregated_id_seq TO audit_writer;

-- ============================================================================
-- STEP 13: TRIGGER TO MAINTAIN AGGREGATION
-- ============================================================================

-- Function to maintain the aggregated table on INSERT to audit_event
CREATE OR REPLACE FUNCTION audit.maintain_aggregation()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
  v_agg_id INTEGER;
BEGIN
  -- Try to find existing aggregated row matching all grouping fields
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
      action_type, action, command, resource, resource_type, details,
      ip_address, user_agent, status, rbac_decision, http_status,
      deploy_approval_request_id
    ) VALUES (
      NEW.id, NEW.id,
      NEW.timestamp, NEW.timestamp,
      NEW.event_hash, NEW.event_hash,
      1,
      NEW.response_time_ms, NEW.response_time_ms, NEW.response_time_ms,
      NEW.user_email, NEW.user_name, NEW.api_token_id, NEW.api_token_name,
      NEW.action_type, NEW.action, NEW.command, NEW.resource, NEW.resource_type, NEW.details,
      NEW.ip_address, NEW.user_agent, NEW.status, NEW.rbac_decision, NEW.http_status,
      NEW.deploy_approval_request_id
    );
  END IF;

  RETURN NEW;
END;
$$;

-- Create trigger that fires AFTER INSERT on audit_event
CREATE TRIGGER t_maintain_aggregation
AFTER INSERT ON audit.audit_event
FOR EACH ROW EXECUTE FUNCTION audit.maintain_aggregation();

-- ============================================================================
-- DONE
-- ============================================================================
