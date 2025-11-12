#!/bin/sh
set -e

# Security: Unset admin database credentials before starting the gateway service.
# The gateway should only have access to DATABASE_URL (restricted user),
# not ADMIN_DATABASE_URL (full permissions).
unset ADMIN_DATABASE_URL

# Start the gateway API server
exec ./rack-gateway-api
