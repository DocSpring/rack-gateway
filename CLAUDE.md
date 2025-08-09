# Convox Gateway - Technical Details

**IMPORTANT: Read CONVOX_REFERENCE.md and README.md first for context on how Convox actually works and current project status.**

## Project Overview

This is a SOC 2 compliant authentication and authorization proxy for self-hosted Convox racks. It sits between users and the Convox API, adding:

- Google Workspace OAuth authentication
- Role-based access control (RBAC)
- Complete audit logging with automatic secret redaction
- Multi-rack support (staging, US, EU, etc.)

## Architecture

```
Developer Machine -> Gateway Server (Admin-run) -> Convox Racks
                     |
                     v
                  Audit Logs -> CloudWatch

Flow:
1. Developer runs: convox-gateway apps
2. CLI loads JWT from ~/.config/convox-gateway/config.json
3. CLI sets RACK_URL with JWT and calls real convox CLI
4. Request goes to Gateway API Server with JWT auth
5. Gateway validates JWT and checks RBAC permissions
6. Gateway proxies to real Convox rack using actual token
7. Gateway logs request to CloudWatch
8. Response flows back through gateway to developer
```

## Key Implementation Details

### Distributed Architecture

**Gateway Server (Admin-managed):**

- Runs on dedicated infrastructure
- Has access to real Convox rack credentials
- Configured via environment variables (RACK*URL*_, RACK*TOKEN*_)
- Handles OAuth, RBAC, and audit logging

**Developer CLI (User-installed):**

- Installed as binary at `/usr/local/bin/convox-gateway`
- Wraps the real `convox` CLI
- Stores config in `~/.config/convox-gateway/config.json`
- Never has direct access to Convox rack tokens

### Authentication Flow

1. User runs `convox-gateway login staging https://convox-gateway.company.com`
2. CLI opens browser for Google OAuth (PKCE flow)
3. User authenticates with Google Workspace account
4. Gateway validates domain (@company.com)
5. Gateway issues JWT token (30 day TTL)
6. CLI stores gateway URL and token in `~/.config/convox-gateway/config.json`

### Authorization (RBAC)

- Uses Casbin v2 for policy enforcement
- Roles: viewer, ops, deployer, admin
- Permissions format: `convox:{resource}:{action}`
- Wildcard support for admin role
- Policies stored in YAML, hot-reloadable

### Proxy Behavior

- Never exposes real Convox rack tokens to clients
- Injects rack token from environment variables
- Adds tracing headers: X-User-Email, X-Request-ID
- Forwards all methods: GET, POST, PUT, PATCH, DELETE

### Security Features

- JWT tokens with HS256 signing (ES256 ready)
- Automatic secret redaction in logs
- Domain restriction for OAuth
- CSRF protection on web UI
- All secrets in env vars or KMS

## Important Assumptions Made

**⚠️ These are educated guesses - verify against actual Convox source:**

1. **Authentication**: Assumed Convox uses Bearer token auth
2. **API Paths**: Guessed common paths like `/apps`, `/ps`, `/env`, `/logs`
3. **Token Format**: Assumed standard Authorization header
4. **Response Format**: Assumed JSON responses

## TODO - Verify with Actual Convox

Check these against `../convox_rack` and `../convox` source:

- [ ] Actual authentication header format
- [ ] Real API endpoints and paths
- [ ] Request/response body formats
- [ ] Error response structures
- [ ] Websocket requirements for logs/exec
- [ ] Rate limiting considerations

## Build Commands

```bash
make all          # Build everything
make gateway      # Build gateway server
make cli          # Build CLI tool
make test         # Run tests
make dev          # Run gateway in dev mode
make docker       # Build Docker image
```

## Testing Status

✅ **What's Tested:**

- JWT creation/validation
- RBAC permission checks
- Audit log redaction
- Integration test (server starts, endpoints respond)

❌ **Not Tested (requires external deps):**

- Real Google OAuth flow
- Actual Convox rack proxying
- Multi-rack switching
- Web UI interactions

## Configuration Files

### Server Configuration

- `config/policies.yaml` - RBAC policies
- `config/users.yaml` - User→role mappings (auto-created)
- `config/roles.yaml` - Role→permission mappings (auto-created)

### Client Configuration

- `~/.config/convox-gateway/config.json` - Gateway URLs and JWT tokens per rack

## Environment Variables

Critical for production:

- `APP_JWT_KEY` - JWT signing key (auto-generated in dev)
- `GOOGLE_CLIENT_ID` - OAuth client ID
- `GOOGLE_CLIENT_SECRET` - OAuth client secret
- `GOOGLE_ALLOWED_DOMAIN` - Email domain restriction
- `RACK_URL_*` - Convox rack API URLs (e.g. RACK_URL_STAGING)
- `RACK_TOKEN_*` - Actual Convox rack API tokens (e.g. RACK_TOKEN_STAGING)

## Deployment Notes

### Convox Deployment

- Use `convox.yml` for app definition
- Enable sticky sessions for OAuth flow
- Set all env vars via `convox env set`

### Terraform Integration

- KMS key for secrets encryption
- CloudWatch log group (90 day retention)
- SSM parameters for secure token storage
- Module in `terraform/main.tf`

### CloudWatch Integration

- Structured JSON logs to stdout
- Automatic ingestion via Convox
- Create metric filters for denied requests
- Alert on spikes in rbac_decision="deny"

## Code Structure

```
internal/
  gateway/
    auth/     - OAuth + JWT handling
    rbac/     - Casbin-based authorization
    proxy/    - Request forwarding logic
    audit/    - Structured logging + redaction
    config/   - Configuration management
    ui/       - Admin web interface
```

## Known Limitations

1. **No Websocket Support** - Logs/exec might not work
2. **Simple User Store** - Currently YAML, needs database for production
3. **No User Self-Service** - Admin must add users manually
4. **Basic UI** - Minimal functionality, needs enhancement
5. **No Metrics** - Should add Prometheus/OpenTelemetry

## Security Considerations

1. **JWT Key Rotation** - Not implemented, needed for production
2. **Token Refresh** - No refresh tokens, users re-auth after 30 days
3. **Audit Log Encryption** - Relies on CloudWatch/KMS
4. **Rate Limiting** - Not implemented, needed for production

## Next Steps for Production

1. Verify actual Convox API behavior
2. Add database for user/role storage
3. Implement JWT key rotation
4. Add Prometheus metrics
5. Enhance web UI
6. Add integration tests with mock Convox
7. Set up CI/CD pipeline
8. Add OpenTelemetry tracing

## Useful Commands for Development

```bash
# Run with hot reload
air

# Check for security issues
gosec ./...

# Generate mocks for testing
mockgen -source=internal/rbac/rbac.go -destination=internal/rbac/mock_rbac.go

# Profile memory usage
go tool pprof http://localhost:8080/debug/pprof/heap

# Check test coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Related Documentation

- [Convox Rack API](https://docs.convox.com/reference/rack-api) (if it exists)
- [Casbin Documentation](https://casbin.org/)
- [Google OAuth 2.0](https://developers.google.com/identity/protocols/oauth2)
- [JWT Best Practices](https://tools.ietf.org/html/rfc8725)

## CRITICAL SAFETY WARNINGS

### NEVER DELETE THESE DIRECTORIES

**These contain LIVE Convox configuration backups that protect the user's actual production settings:**

- `/Users/*/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP`
- Any backup directory in `/Users/*/Library/Preferences/`

The integration tests create backups of the real Convox CLI configuration to prevent data loss. If tests fail with "backup already exists", it means a previous test didn't clean up properly. The user must manually verify and move/restore the backup - NEVER automatically delete it!
