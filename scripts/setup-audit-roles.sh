#!/usr/bin/env bash
set -euo pipefail

# Create the audit roles (scripts/audit-roles.sql) for dev/test databases.
# In production these are created by Terraform (see reference/convox_racks_terraform)

# Try DATABASE_URL first, fall back to E2E_DATABASE_URL, then TEST_DATABASE_URL, then default
DATABASE_URL="${DATABASE_URL:-${E2E_DATABASE_URL:-${TEST_DATABASE_URL:-postgres://postgres:postgres@localhost:55432/gateway_test?sslmode=disable}}}"

echo "Setting up audit roles..."
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -f "$(dirname "${BASH_SOURCE[0]}")/audit-roles.sql"
echo "✓ Audit roles configured successfully"
