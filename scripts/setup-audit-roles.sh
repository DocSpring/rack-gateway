#!/usr/bin/env bash
set -euo pipefail

# Setup audit roles for CI/test environments
# In production these are created by Terraform (see reference/convox_racks_terraform)

# Try DATABASE_URL first, fall back to E2E_DATABASE_URL, then TEST_DATABASE_URL, then default
DATABASE_URL="${DATABASE_URL:-${E2E_DATABASE_URL:-${TEST_DATABASE_URL:-postgres://postgres:postgres@localhost:55432/gateway_test?sslmode=disable}}}"

echo "Setting up audit roles for test/CI environment..."

# Create the three audit roles (IF NOT EXISTS for idempotency)
# These match the Terraform postgresql_role resources
psql "$DATABASE_URL" <<'SQL'
-- Create audit_owner role (owner of audit schema and objects)
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_owner') THEN
    CREATE ROLE audit_owner NOLOGIN;
    RAISE NOTICE 'Created role audit_owner';
  ELSE
    RAISE NOTICE 'Role audit_owner already exists';
  END IF;
END
$$;

-- Create audit_writer role (for inserting audit events)
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_writer') THEN
    CREATE ROLE audit_writer NOLOGIN;
    RAISE NOTICE 'Created role audit_writer';
  ELSE
    RAISE NOTICE 'Role audit_writer already exists';
  END IF;
END
$$;

-- Create audit_reader role (for reading audit events)
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_reader') THEN
    CREATE ROLE audit_reader NOLOGIN;
    RAISE NOTICE 'Created role audit_reader';
  ELSE
    RAISE NOTICE 'Role audit_reader already exists';
  END IF;
END
$$;

-- Grant audit_owner to postgres user (equivalent to rack_gateway_admin in production)
-- This allows migrations to create objects owned by audit_owner
GRANT audit_owner TO postgres;

SQL

echo "✓ Audit roles configured successfully"
