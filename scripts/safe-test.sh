#!/bin/bash

# This script safely runs tests by managing Convox config backup/restore
set -euo pipefail

# Get script directory to find backup/restore scripts
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_SCRIPT="$SCRIPT_DIR/backup-convox-config.sh"
RESTORE_SCRIPT="$SCRIPT_DIR/restore-convox-config.sh"

# CRITICAL: This is the REAL Convox config - DO NOT DELETE
BACKUP_PATH="$HOME/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP"

# Set env var to indicate tests are running through the safe wrapper
export CONVOX_GATEWAY_SAFE_TEST=1

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
go test "$@"
