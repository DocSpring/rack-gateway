#!/bin/bash

# This script safely runs tests by managing Convox config backup/restore
set -euo pipefail

# CRITICAL: This is the REAL Convox config - DO NOT DELETE
BACKUP_PATH="$HOME/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP"
CONFIG_PATH="$HOME/Library/Preferences/convox"

# Set env var to indicate tests are running through the safe wrapper
export CONVOX_GATEWAY_SAFE_TEST=1

# Track if restore has already been called
RESTORE_DONE=0

# Function to restore config
# shellcheck disable=SC2329
restore_config() {
    # Prevent double restoration
    if [ "$RESTORE_DONE" -eq 1 ]; then
        return 0
    fi
    RESTORE_DONE=1
    
    if [ ! -d "$BACKUP_PATH" ]; then
        # Only warn if we're not in CI and the backup should exist
        if [ -z "${CI:-}" ]; then
            echo -e "\033[31mCRITICAL: No backup found at $BACKUP_PATH!\033[0m"
            echo -e "\033[31mYour Convox config may need manual restoration\033[0m"
            exit 1
        fi
        return 0
    fi
    
    # Remove placeholder if it exists
    if [ -e "$CONFIG_PATH" ]; then
        rm -rf "$CONFIG_PATH"
    fi
    
    echo -e "\033[33mSAFETY: Restoring Convox config from $BACKUP_PATH to $CONFIG_PATH\033[0m"
    mv "$BACKUP_PATH" "$CONFIG_PATH" || {
        echo -e "\033[31mCRITICAL: Failed to restore Convox config - YOU MUST MANUALLY RESTORE YOUR CONVOX CONFIG\033[0m"
        echo "Run: mv $BACKUP_PATH $CONFIG_PATH"
        exit 1
    }
}

if [ -n "${CI:-}" ]; then
    echo "Running in CI, skipping config backup/restore"
else
    # Check if backup already exists
    if [ -e "$BACKUP_PATH" ]; then
        echo -e "\033[31mCRITICAL: Backup already exists at $BACKUP_PATH - refusing to run to prevent data loss\033[0m"
        echo "Please manually resolve this before running tests"
        exit 1
    fi

    # Check if config exists
    if [ ! -e "$CONFIG_PATH" ]; then
        echo -e "\033[31mCRITICAL: Convox config does not exist at $CONFIG_PATH\033[0m"
        exit 1
    fi

    # Backup config
    echo -e "\033[33mSAFETY: Backing up Convox config from $CONFIG_PATH to $BACKUP_PATH\033[0m"
    mv "$CONFIG_PATH" "$BACKUP_PATH" || {
        echo -e "\033[31mCRITICAL: Failed to backup Convox config\033[0m"
        exit 1
    }

    # Create placeholder so tests don't fail due to missing dir
    mkdir -p "$CONFIG_PATH"
    echo "TEST_PLACEHOLDER" > "$CONFIG_PATH/GUARD_ACTIVE"

    # Trap to ensure restore happens on exit
    trap restore_config EXIT INT TERM
fi

# Run the actual tests
echo "Running tests with Convox config protection..."
go test "$@"
