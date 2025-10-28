#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

E2E_DATABASE_NAME="${E2E_DATABASE_NAME:-gateway_test}"
E2E_GATEWAY_SERVICE="${E2E_GATEWAY_SERVICE:-gateway-api-test}"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/lib/cli-e2e/db.sh"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/lib/cli-e2e/mfa.sh"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/lib/cli-e2e/cli_helpers.sh"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/lib/cli-e2e/stages.sh"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/lib/cli-e2e/suite.sh"

trap 'reset_all_test_state || true' EXIT

E2E_TS="$(date +%s%3N)"

mkdir -p config/cli-e2e
export GATEWAY_CLI_CONFIG_DIR="config/cli-e2e"

run_cli_e2e_suite
