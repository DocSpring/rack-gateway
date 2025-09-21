# Convox Gateway

API proxy for Convox racks with SSO, RBAC, and audit logging

## 📖 Start Here

**First time setup?** Follow these steps:

1. **Quick Start** (below) - Gets you running in 5 minutes with mock services
2. **[AGENTS.md](AGENTS.md)** - Technical implementation details for AI agents
3. **[docs/DEV.md](docs/DEV.md)** - Development guide

## Other Documentation

- **[docs/DEPLOY.md](docs/DEPLOY.md)** - Production deployment guide
- **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)** - All environment variables and options
- **[docs/CONVOX_REFERENCE.md](docs/CONVOX_REFERENCE.md)** - How Convox works

## Features

- **Google Workspace OAuth**: Secure authentication with domain restrictions
- **Role-Based Access Control**: Granular permissions (viewer, ops, deployer, admin)
- **Audit Logging**: Complete activity logs with automatic secret redaction
- **Multi-Rack Support**: Manage staging, US, EU, and other racks
- **Session Management**: 30-day JWT sessions with secure token storage
- **Minimal Web UI**: User and role management interface

## Quick Start

### ⚡ 5-Minute Setup (Mock Services)

Get everything running locally with mock services - no Google OAuth setup required:

```bash
# 1. Clone and install
git clone https://github.com/DocSpring/convox-gateway.git
cd convox-gateway
go mod download
cd web && pnpm install && cd ..

# 2. Set up configuration (uses defaults with mock services)
cp mise.local.toml.example mise.local.toml

# 3. Start everything
task dev
```

**🎉 You're done!** Open these URLs:

- **Web UI**: http://localhost:5223 (test users: admin@example.com, deployer@example.com, ops@example.com, viewer@example.com)
- **Gateway API**: http://localhost:8447
- **Mock Convox**: http://localhost:5443

### 🧪 Test the CLI

```bash
# Login (opens mock OAuth in browser)
./bin/convox-gateway login staging http://localhost:8447

# Run convox commands through the gateway
./bin/convox-gateway convox apps
./bin/convox-gateway convox ps
```

### Prerequisites

- Go 1.22+
- Docker & Docker Compose
- Node.js 20+ and pnpm
- mise (for environment variables) - [Install mise](https://mise.jdx.dev/getting-started.html)

Install the task runner (recommended):

```bash
# macOS (Homebrew)
brew install go-task/tap/go-task

# Linux (curl)
curl -sL https://taskfile.dev/install.sh | sh -s -- -b /usr/local/bin

# Or use the convenience script
sh scripts/dev-setup.sh
```

### Building

```bash
# Build everything
task build

# Individual targets
task go:build:gateway # Build gateway API server -> bin/convox-gateway-api
task go:build:cli     # Build gateway CLI -> bin/convox-gateway
task docker     # Build Docker image
task test       # Run all tests
```

### Real Google OAuth Setup

For complete development setup with real Google OAuth (instead of mock), see **[docs/DEV.md](docs/DEV.md)**.

**Development URLs:**

- Gateway API: http://localhost:8447
- Web UI: http://localhost:5223
- Mock Convox: http://localhost:5443
- Mock OAuth: http://localhost:3001

The development environment includes a mock Google OAuth server that simulates the authentication flow with test users:

- **admin@example.com** - Admin User (full access)
- **deployer@example.com** - Deployer User (full deployment permissions including env vars)
- **ops@example.com** - Ops User (manage processes and view environments)
- **viewer@example.com** - Viewer User

When logging in via http://localhost:8447 during development, you'll be redirected to the mock OAuth server to select which test user to authenticate as.

## How It Works

The gateway acts as a transparent proxy that speaks the Convox API protocol. It accepts JWT tokens (for developers) or API tokens (for CI/CD) as authentication.

### Two Ways to Use the Gateway

#### Option 1: Native Convox CLI (Direct)

```bash
# For CI/CD with API token
export RACK_URL="https://convox:<api-token>@gateway.example.com"
convox apps  # Uses standard convox CLI directly

# For developers with JWT token
export RACK_URL="https://convox:<jwt-token>@gateway.example.com"
convox apps  # Uses standard convox CLI directly
```

#### Option 2: convox-gateway CLI Wrapper (Convenience)

```bash
# Use our wrapper for easier multi-rack management
convox-gateway login staging https://gateway.example.com
convox-gateway convox apps  # Automatically sets RACK_URL with stored token

# Set up convenient shell alias
alias cx="convox-gateway convox"
cx apps
cx ps
cx deploy
```

The `convox-gateway` CLI wrapper is optional - it just provides:

- Automatic token management
- Multi-rack configuration
- Browser-based login flow
- Token expiry reminders

## CLI Usage

### Setup

```bash
# Login to a rack (sets it as current)
convox-gateway login staging https://gateway.example.com
# Opens browser for Google OAuth
# Stores configuration in ~/.config/convox-gateway/config.json
```

### Running Convox Commands

```bash
# All convox commands go through "convox-gateway convox"
convox-gateway convox apps
convox-gateway convox ps
convox-gateway convox deploy

# With the cx alias:
cx apps
cx ps
cx deploy
cx logs -f
```

### Managing Racks

```bash
# Show current rack and status
convox-gateway rack

# List all configured racks
convox-gateway racks

# Switch to a different rack
convox-gateway switch production

# With the cg alias:
cg rack
cg racks
cg switch eu-west
```

### Rack Selection

The CLI determines which rack to use in this order:

1. `--rack` flag: `convox-gateway --rack production convox apps`
2. Environment variable: `CONVOX_GATEWAY_RACK=production cx apps`
3. Current rack from `~/.config/convox-gateway/current`

### Generate shell completions:

```bash
# Bash
source <(./bin/convox-gateway completion bash)

# Zsh
source <(./bin/convox-gateway completion zsh)

# Fish
./bin/convox-gateway completion fish | source
```

## Configuration

See docs/CONFIGURATION.md for the full list of environment variables and configuration options.

### Database

The gateway uses Postgres to store users, API tokens, and audit logs. Set a connection URL via `DATABASE_URL` (or equivalent `PG*` vars):

```bash
DATABASE_URL=postgres://user:pass@host:5432/dbname?sslmode=require
```

The database is automatically initialized on first run with an admin user from the first Google OAuth login.

The CLI stores its configuration separately:

- `~/.config/convox-gateway/config.json`: Local CLI configuration (per developer)

## RBAC Model

### Roles

- **viewer**: Read-only access to apps, processes, and logs
- **ops**: Restart apps, view env (via releases), manage processes
- **deployer**: Full deployment permissions (builds/releases), non-destructive
- **admin**: Complete access to all operations

### Permissions

Format: `convox:{resource}:{action}`

Examples:

- `convox:app:list` - List applications
- `convox:app:restart` - Restart an application
- `convox:process:exec` - Exec into a running process

## Audit Logging

All API calls are logged to stdout in structured JSON format:

```json
{
  "ts": "2024-01-15T10:30:00Z",
  "user_email": "user@your-domain.com",
  "rack": "staging",
  "method": "POST",
  "path": "/apps/myapp/restart",
  "status": 200,
  "latency_ms": 234,
  "rbac_decision": "allow",
  "request_id": "uuid",
  "client_ip": "192.168.1.1"
}
```

### Automatic Redaction

Sensitive data is automatically redacted:

- Passwords, tokens, API keys
- Authorization headers
- Environment variable values
- Any field matching sensitive patterns

## Deployment

### Docker

```bash
task docker
docker run -p 8080:8080 \
  -e GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID \
  -e GOOGLE_CLIENT_SECRET=$GOOGLE_CLIENT_SECRET \
  -e RACK_TOKEN=$RACK_TOKEN \
  convox-gateway-api:latest
```

### Convox

```bash
convox apps create convox-gateway
convox env set GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID -a convox-gateway
convox env set GOOGLE_CLIENT_SECRET=$GOOGLE_CLIENT_SECRET -a convox-gateway
convox deploy -a convox-gateway
```

## CloudWatch Configuration

Set log retention:

```bash
aws logs put-retention-policy \
  --log-group-name /convox/your-rack/convox-gateway \
  --retention-in-days 90
```

Create metric filters for security monitoring:

```bash
aws logs put-metric-filter \
  --log-group-name /convox/your-rack/convox-gateway \
  --filter-name rbac-denies \
  --filter-pattern '[..., rbac_decision="deny", ...]' \
  --metric-transformations \
    metricName=RBACDenies,metricNamespace=ConvoxAuth,metricValue=1
```

## Security Considerations

1. **JWT Keys**: Use strong, unique keys in production
2. **Domain Restriction**: Enforce Google Workspace domain
3. **TLS**: Always use HTTPS in production
4. **Rack Tokens**: Store securely, rotate regularly
5. **Audit Logs**: Monitor for anomalies and denied requests
6. **Session Duration**: 30-day default, adjust as needed

## Testing

Run unit tests:

```bash
task test
```

Run linters:

```bash
task lint
```

Run integration test:

```bash
./scripts/integration_test.sh
```

Run end-to-end test (deprecated):

```bash
./scripts/e2e_test.sh
```

## Troubleshooting

### Login Issues

1. Verify Google OAuth credentials
2. Check redirect URL configuration
3. Ensure domain restriction matches your email

### Permission Denied

1. Check user role assignments
2. Verify RBAC policies
3. Review audit logs for details

### Rack Connection Failed

1. Verify rack tokens are correct
2. Check rack URLs in config
3. Ensure network connectivity

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes with tests
4. Run `task test` and `task lint`
5. Submit pull request

## License

MIT License - See LICENSE file for details

## Support

For issues or questions:

- Create an issue on GitHub
- Check audit logs for debugging

## Deployment

See DEPLOY.md for a production-ready deployment guide, environment configuration, persistence, and a minimal `convox.yml` example.

- Review CloudWatch logs for errors
