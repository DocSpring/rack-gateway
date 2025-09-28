#!/bin/bash

# This script safely runs tests by managing Convox config backup/restore
set -euo pipefail

# Get script directory to find backup/restore scripts
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_SCRIPT="$SCRIPT_DIR/backup-convox-config.sh"
RESTORE_SCRIPT="$SCRIPT_DIR/restore-convox-config.sh"

# CRITICAL: This is the REAL Convox config - DO NOT DELETE
BACKUP_PATH="$HOME/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP"

# Determine Convox config dir (match internal/testutil/convoxguard logic)
detect_config_dir() {
    case "$(uname -s)" in
        Darwin)
            echo "$HOME/Library/Preferences/convox"
            ;;
        Linux)
            local xdg="${XDG_CONFIG_HOME:-}"
            if [ -z "$xdg" ]; then
                xdg="$HOME/.config"
            fi
            echo "$xdg/convox"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            # Best-effort for Windows shells
            if [ -n "${LOCALAPPDATA:-}" ]; then
                echo "$LOCALAPPDATA/convox"
            else
                echo "$HOME/.config/convox"
            fi
            ;;
        *)
            echo "$HOME/.config/convox"
            ;;
    esac
}

CONVOX_CFG_DIR="$(detect_config_dir)"
GUARD_FILE="$CONVOX_CFG_DIR/GUARD_ACTIVE"

# Set env var to indicate tests are running through the safe wrapper
export GATEWAY_SAFE_TEST=1

# Track if restore has already been called and if we created the backup
RESTORE_DONE=0
BACKUP_CREATED_BY_US=0

# Function to restore config using the restore script
# shellcheck disable=SC2329
restore_config() {
    # Prevent double restoration
    if [ "$RESTORE_DONE" -eq 1 ]; then
        return 0
    fi
    RESTORE_DONE=1

    # Only restore if we created the backup
    if [ "$BACKUP_CREATED_BY_US" -eq 0 ]; then
        return 0
    fi

    if [ ! -d "$BACKUP_PATH" ]; then
        echo -e "\033[31mCRITICAL: No backup found at $BACKUP_PATH!\033[0m"
        echo -e "\033[31mYour Convox config may need manual restoration\033[0m"
        exit 1
    fi

    echo -e "\033[33mSAFETY: Running restore script...\033[0m"
    "$RESTORE_SCRIPT" || {
        echo -e "\033[31mCRITICAL: Failed to restore Convox config - YOU MUST MANUALLY RESTORE YOUR CONVOX CONFIG\033[0m"
        echo "Run: $RESTORE_SCRIPT"
        exit 1
    }
}

if [ -n "${CI:-}" ]; then
    echo "Running in CI, skipping config backup/restore"
elif [ -d "$BACKUP_PATH" ]; then
    BACKUP_CREATED_BY_US=0
else
    # Run backup using the backup script
    echo -e "\033[33mSAFETY: Running backup script...\033[0m"
    "$BACKUP_SCRIPT" || {
        echo -e "\033[31mCRITICAL: Failed to backup Convox config\033[0m"
        exit 1
    }
    BACKUP_CREATED_BY_US=1

    # Trap to ensure restore happens on exit
    trap restore_config EXIT INT TERM
fi

# Run the actual tests
echo "Running tests with Convox config protection..."

# Create guard file to signal wrapper is active (cleaned up by restore)
mkdir -p "$CONVOX_CFG_DIR"
touch "$GUARD_FILE"

if [ -n "${TEST_DATABASE_URL:-}" ]; then
  echo "Setting DATABASE_URL to ${TEST_DATABASE_URL}"
  export DATABASE_URL="${TEST_DATABASE_URL}"
fi

ensure_local_database() {
    if ! command -v python3 >/dev/null 2>&1; then
        return
    fi
    if [ -z "${DATABASE_URL:-}" ]; then
        return
    fi

    local IFS=$'\n'
    read -r db_name admin_uri host port <<'EOF'
$(python3 <<'PY'
import os
from urllib.parse import urlsplit, urlunsplit

uri = os.environ.get("DATABASE_URL")
if not uri:
    raise SystemExit

parts = urlsplit(uri)
dbname = parts.path.lstrip('/')
if not dbname:
    raise SystemExit

admin_parts = parts._replace(path='/postgres')
admin_uri = urlunsplit(admin_parts)
print(dbname)
print(admin_uri)
print(parts.hostname or '')
print(parts.port or '')
PY
)
EOF

    if [ -z "$db_name" ] || [ -z "$admin_uri" ]; then
        return
    fi

    local use_docker=false
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        case "$host" in
            ''|localhost|127.0.0.*)
                if [ "${port:-}" = "55432" ]; then
                    docker compose up -d --pull never postgres >/dev/null 2>&1 || true
                    for _ in $(seq 1 20); do
                        if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
                            use_docker=true
                            break
                        fi
                        sleep 1
                    done
                fi
                ;;
        esac
    fi

    if [ "$use_docker" = true ]; then
        if docker compose exec -T postgres psql -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${db_name}'" | grep -q 1; then
            return
        fi

        echo "Creating local database $db_name for tests (docker compose)..."
        if ! docker compose exec -T postgres psql -U postgres -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${db_name}\";" >/dev/null; then
            echo "Warning: failed to create database $db_name in docker (continuing)." >&2
        fi
        return
    fi

    case "$host" in
        ''|localhost|127.0.0.*)
            ;;
        *)
            # Avoid touching remote databases
            return
            ;;
    esac

    if ! command -v psql >/dev/null 2>&1; then
        return
    fi

    if psql "$admin_uri" -tAc "SELECT 1 FROM pg_database WHERE datname='${db_name}'" | grep -q 1; then
        return
    fi

    echo "Creating local database $db_name for tests..."
    if ! psql "$admin_uri" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${db_name}\";" >/dev/null; then
        echo "Warning: failed to create database $db_name (continuing)." >&2
    fi
}

ensure_local_database

go test "$@"
