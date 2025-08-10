#!/bin/bash

set -e

CONVOX_CONFIG_PATH="$HOME/.convox"
BACKUP_BASE_PATH="$HOME/Library/Preferences"
BACKUP_PATH="$BACKUP_BASE_PATH/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP"

if [[ ! -d "$BACKUP_PATH" ]]; then
    echo "No backup found at $BACKUP_PATH - nothing to restore"
    exit 0
fi

if [[ -d "$CONVOX_CONFIG_PATH" ]]; then
    echo "Warning: Current Convox configuration exists at $CONVOX_CONFIG_PATH"
    echo "This will be replaced with the backup. Continue? (y/N)"
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
        echo "Restore cancelled"
        exit 0
    fi
    rm -rf "$CONVOX_CONFIG_PATH"
fi

echo "Restoring Convox configuration from backup..."
cp -r "$BACKUP_PATH" "$CONVOX_CONFIG_PATH"
echo "Configuration restored to $CONVOX_CONFIG_PATH"

echo "Removing backup..."
rm -rf "$BACKUP_PATH"
echo "Backup removed from $BACKUP_PATH"
echo "Convox configuration successfully restored"
