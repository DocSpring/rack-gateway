-- Cluster-wide audit roles the migrations expect. Production creates them with Terraform
-- (postgresql_role resources); dev, test and CI create them from this file. Safe to run repeatedly.
SET client_min_messages = warning;

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_owner') THEN
    CREATE ROLE audit_owner NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_writer') THEN
    CREATE ROLE audit_writer NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_reader') THEN
    CREATE ROLE audit_reader NOLOGIN;
  END IF;
END
$$;

-- Lets migrations create objects owned by audit_owner (rack_gateway_admin holds it in production).
GRANT audit_owner TO postgres;
