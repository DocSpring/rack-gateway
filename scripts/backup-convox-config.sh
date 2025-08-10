#!/bin/bash

set -e

CONVOX_CONFIG_PATH="$HOME/.convox"
BACKUP_BASE_PATH="$HOME/Library/Preferences"
BACKUP_PATH="$BACKUP_BASE_PATH/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP"

if [[ ! -d "$CONVOX_CONFIG_PATH" ]]; then
    echo "No Convox configuration found at $CONVOX_CONFIG_PATH - nothing to backup"
    exit 0
fi

if [[ -d "$BACKUP_PATH" ]]; then
    echo "Backup already exists at $BACKUP_PATH"
    echo "This suggests a previous test didn't clean up properly."
    echo "Please manually verify and move/restore the backup before running tests."
    exit 1
fi

echo "Creating backup of Convox configuration..."
mkdir -p "$BACKUP_BASE_PATH"
cp -r "$CONVOX_CONFIG_PATH" "$BACKUP_PATH"
echo "Backup created at $BACKUP_PATH"

echo "Removing original Convox configuration to prevent interference..."
rm -rf "$CONVOX_CONFIG_PATH"
echo "Original configuration backed up and removed"