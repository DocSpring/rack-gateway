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
User → CLI → Auth Proxy → Convox Rack API
              ↓
         Audit Logs → CloudWatch
```

## Key Implementation Details

### Authentication Flow
1. User runs `convox-gateway login staging`
2. CLI opens browser for Google OAuth (PKCE flow)
3. User authenticates with Google Workspace account
4. Proxy validates domain (@docspring.com)
5. Proxy issues JWT token (30 day TTL)
6. CLI stores token locally in `~/.config/convox-gateway/tokens.json`

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
make proxy        # Build proxy server
make cli          # Build CLI tool
make test         # Run tests
make dev          # Run proxy in dev mode
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

- `config/racks.yaml` - Rack definitions (URL, region, tokens)
- `config/policies.yaml` - RBAC policies
- `config/users.yaml` - User→role mappings (auto-created)
- `config/roles.yaml` - Role→permission mappings (auto-created)

## Environment Variables

Critical for production:
- `APP_JWT_KEY` - JWT signing key (auto-generated in dev)
- `GOOGLE_CLIENT_ID` - OAuth client ID
- `GOOGLE_CLIENT_SECRET` - OAuth client secret
- `GOOGLE_ALLOWED_DOMAIN` - Email domain restriction
- `RACK_TOKEN_*` - Actual Convox rack API tokens

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