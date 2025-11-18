-- Allow release_id to be updated from empty string to a value
-- This is needed because the real Convox API returns builds with empty release field initially

CREATE OR REPLACE FUNCTION validate_deploy_approval_state_machine()
RETURNS TRIGGER AS $$
BEGIN
  -- Only validate updates (not inserts)
  IF TG_OP = 'INSERT' THEN
    RETURN NEW;
  END IF;

  -- Rule 1: object_url can only be set when status='approved' and object_url is currently NULL
  IF NEW.object_url IS DISTINCT FROM OLD.object_url AND NEW.object_url IS NOT NULL THEN
    IF OLD.status != 'approved' THEN
      RAISE EXCEPTION 'object_url can only be set when status is approved (current status: %)', OLD.status;
    END IF;
    IF OLD.object_url IS NOT NULL THEN
      RAISE EXCEPTION 'object_url can only be set once';
    END IF;
  END IF;

  -- Rule 2: build_id can only be set when object_url exists and build_id is currently NULL
  IF NEW.build_id IS DISTINCT FROM OLD.build_id AND NEW.build_id IS NOT NULL THEN
    IF NEW.object_url IS NULL THEN
      RAISE EXCEPTION 'build_id can only be set after object_url is set';
    END IF;
    IF OLD.build_id IS NOT NULL THEN
      RAISE EXCEPTION 'build_id can only be set once';
    END IF;
  END IF;

  -- Rule 3: release_id can only be set when build_id exists
  -- Allow updating from empty string to a value (real Convox API behavior)
  IF NEW.release_id IS DISTINCT FROM OLD.release_id AND NEW.release_id IS NOT NULL AND NEW.release_id != '' THEN
    IF NEW.build_id IS NULL THEN
      RAISE EXCEPTION 'release_id can only be set after build_id is set';
    END IF;
    IF OLD.release_id IS NOT NULL AND OLD.release_id != '' THEN
      RAISE EXCEPTION 'release_id can only be set once';
    END IF;
  END IF;

  -- Rule 4: status can only change to 'deployed' when release_id exists
  IF NEW.status IS DISTINCT FROM OLD.status AND NEW.status = 'deployed' THEN
    IF NEW.release_id IS NULL OR NEW.release_id = '' THEN
      RAISE EXCEPTION 'status can only be set to deployed after release_id is set';
    END IF;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
