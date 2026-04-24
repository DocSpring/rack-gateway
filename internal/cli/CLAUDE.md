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
4. **Convox wrapper**: Wraps `convox` CLI commands

## Configuration

Config file location: `~/.config/rack-gateway/config.json`

Structure:

```json
{
  "current": "production",
  "gateways": {
    "production": {
      "url": "https://gateway-prod.example.com",
      "token": "session-token-here",
      "email": "user@example.com",
      "expires_at": "2025-12-31T23:59:59Z",
      "mfa_preference": "webauthn",
      "notification_sound": "default",
      "sound_volume": 0.8
    },
    "staging": {
      "url": "https://gateway-staging.example.com",
      "token": "session-token-here"
    },
    "dev": {
      "url": "http://localhost:8447"
    }
  },
  "mfa_preference": "webauthn",
  "notification_sound": "default",
  "sound_volume": 0.6,
  "all_racks_exclude": ["dev", "Dev"]
}
```

**Configuration Fields:**

**Global Settings:**
- `current` - Currently selected rack name
- `gateways` - Map of rack configurations (see GatewayConfig below)
- `machine_id` - Unique machine identifier
- `mfa_preference` - Default MFA method: `"default"`, `"webauthn"`, or `"totp"`
- `notification_sound` - Default notification sound: `"default"`, `"disabled"`, or path to MP3 file
- `sound_volume` - Default sound volume: 0.0 to 1.0 (default: 0.6 = 60%)
- `all_racks_exclude` - Array of rack names to exclude when using `--rack all`

**Per-Rack Settings (GatewayConfig):**
- `url` - Gateway API URL
- `token` - Session authentication token
- `email` - User email address
- `expires_at` - Token expiration timestamp
- `session_id` - Session identifier
- `channel` - WebSocket channel for notifications
- `device_id` - Device identifier
- `device_name` - Device name
- `mfa_verified` - Whether MFA has been verified for this session
- `mfa_preference` - Per-rack MFA method override
- `notification_sound` - Per-rack notification sound override
- `sound_volume` - Per-rack sound volume override (0.0 to 1.0)

**Using --rack all:**

Deploy approval commands support `--rack all` to operate on all configured racks:

```bash
# Wait for deploy approval on all production racks (excluding dev)
cx deploy-approval wait --rack all --commit abc123

# Approve deploy on all racks
cx deploy-approval approve --rack all --commit abc123

# List deploy approval requests from all racks
cx deploy-approval list --rack all
```

The `all` value expands to all configured racks in `gateways`, excluding any racks listed in `all_racks_exclude`. This is useful for avoiding local development racks when operating on production infrastructure.

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
