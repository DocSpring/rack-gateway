# CLI Client - Claude Development Guide

## Overview

The `rack-gateway` CLI is a multi-rack aware wrapper around the `convox` CLI that adds authentication and authorization.

## Architecture

**CLI Client (User-installed, multi-rack aware):**

- Installed as binary at `/usr/local/bin/rack-gateway`
- Wraps the real `convox` CLI
- Stores config for multiple racks in `~/.config/rack-gateway/config.json`
- Uses `--rack` flag to switch between gateways
- Never has direct access to Convox rack tokens

## Key Features

1. **Multi-rack support**: Switch between multiple gateway instances
2. **OAuth flow**: PKCE-based browser authentication
3. **Token management**: Stores session tokens per rack
4. **Convox wrapper**: Transparently wraps `convox` CLI commands

## Configuration

Config file location: `~/.config/rack-gateway/config.json`

Structure:
```json
{
  "racks": {
    "production": {
      "url": "https://gateway-prod.example.com",
      "token": "session-token-here"
    },
    "staging": {
      "url": "https://gateway-staging.example.com",
      "token": "session-token-here"
    }
  },
  "current_rack": "production"
}
```

## Commands

### Login
```bash
rack-gateway login <rack-name> <gateway-url>
```

Opens browser for OAuth flow, stores session token in config.

### Logout
```bash
rack-gateway logout
```

Removes current rack from config.

### Convox Commands
```bash
rack-gateway convox <any-convox-command>
```

Proxies command to Convox API through the gateway with authentication.

## Testing

### E2E Tests

```bash
task go:e2e
```

Tests the complete CLI flow:
- Login via OAuth
- Token storage
- Convox command proxying
- RBAC enforcement
- Logout

**What the tests verify:**
- Admin user can run all commands
- Deployer can deploy but not delete apps
- Ops can view but not modify
- Viewer has minimal permissions

## Development

Build the CLI:
```bash
task go:build
# or
go build -o bin/rack-gateway ./cmd/rack-gateway
```

**⛔ NEVER use `go build` directly in project root** - it creates unwanted binaries. Always use `task go:build`.

## Important Notes

### Safety Warnings

**NEVER DELETE THESE DIRECTORIES:**

- `/Users/*/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP`
- Any backup directory in `/Users/*/Library/Preferences/`

The integration tests create backups of the real Convox CLI configuration to prevent data loss. If tests fail with "backup already exists", the user must manually verify and restore the backup - NEVER automatically delete it!

### OAuth Flow

The CLI uses PKCE (Proof Key for Code Exchange) for secure OAuth without client secrets:

1. Generate code verifier and challenge
2. Open browser to gateway OAuth endpoint
3. User authenticates with Google
4. Gateway validates and returns authorization code
5. CLI exchanges code for session token
6. Token stored in config file

### Error Handling

- Network errors: Suggest checking gateway URL
- Auth errors: Suggest running `rack-gateway login` again
- Permission errors: Clear message about which role is required
- Token expiry: Auto-detect and prompt for re-login
