**Disclaimer:** Convox is a registered trademark of Convox, Inc.
This project is not affiliated with, endorsed by, or sponsored by Convox, Inc.
It’s an independent, open source, community-maintained tool that works with Convox racks.

---

# Rack Gateway

API proxy for Convox racks with SSO, RBAC, and audit logging. SOC2 compliant.

![Screenshot of audit logs](./docs/images/audit-logs.jpg)

_[More screenshots](./docs/SCREENSHOTS.md)_

## What is Convox?

[Convox](https://convox.com) is an open-source PaaS based on Kubernetes available for multiple cloud providers.
The Convox source code is available on GitHub: https://github.com/convox/convox

Convox Console is a closed-source, paid service that provides a web interface for
managing your rack, including user management, RBAC, audit logging, workflows, and more.

Rack Gateway can be thought of as a "community edition" for the hosted Convox Console.
It is an open source, self-hosted interface for managing your rack with OAuth login, RBAC,
audit logs, and secret redaction in env vars. We provide everything you need
for SOC2 compliance on your own self-hosted infrastructure.

**Rack Gateway is provided without any warranty or support. Use at your own risk.**

Sign up for the official Convox Console or inquire about their enterprise license
if you need more features or paid support. Convox Console provides many more advanced features
such as more options for authentication, fine-grained RBAC and custom roles, support for multiple racks,
monitoring and alerts, workflows for CI/CD, etc.

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
- **Single-Tenant Design**: One gateway per rack, deployed alongside Convox API
- **Multi-Rack CLI**: CLI can switch between multiple gateways using `--rack` flag
- **Session Management**: Database-backed sessions with secure token storage
- **Minimal Web UI**: User and role management interface
- **Deploy Approvals**: Manual approval workflow for CI/CD actions (build/object/release/promote)

## Quick Start

### ⚡ 5-Minute Setup (Mock Services)

Get everything running locally with mock services - no Google OAuth setup required:

```bash
# 1. Clone and install
git clone https://github.com/DocSpring/rack-gateway.git
cd rack-gateway
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
./bin/rack-gateway login staging http://localhost:8447

# Run convox commands through the gateway
./bin/rack-gateway convox apps
./bin/rack-gateway convox ps
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
task go:build:gateway # Build gateway API server -> bin/rack-gateway-api
task go:build:cli     # Build gateway CLI -> bin/rack-gateway
task docker     # Build Docker image
task test       # Run all tests
```

### Real Google OAuth Setup

For complete development setup with real Google OAuth (instead of mock), see **[docs/DEV.md](docs/DEV.md)**.

**Development URLs:**

- Gateway API: http://localhost:8447
- Web UI: http://localhost:5223
- Mock Convox: http://localhost:5443
- Mock OAuth: http://localhost:3345 (configurable via `MOCK_OAUTH_PORT`)
- Test Gateway API (preview stack used by E2E): http://localhost:9447
- Test Mock Convox: http://localhost:6443
- Test Mock OAuth: http://localhost:9345

Use `task docker:test:up` to boot the isolated preview-style stack on the test ports. It runs alongside the dev or preview stack while reusing the shared Postgres container.

The development environment includes a mock Google OAuth server that simulates the authentication flow with test users:

- **admin@example.com** - Admin User (full access)
- **deployer@example.com** - Deployer User (full deployment permissions including env vars)
- **ops@example.com** - Ops User (manage processes and view environments)
- **viewer@example.com** - Viewer User

When logging in via http://localhost:8447 during development, you'll be redirected to the mock OAuth server to select which test user to authenticate as.

## How It Works

The gateway acts as a transparent proxy that speaks the Convox API protocol. It accepts session tokens (for developers) or API tokens (for CI/CD) as authentication.

### Two Ways to Use the Gateway

#### Option 1: Native Convox CLI (Direct)

```bash
# For CI/CD with API token
export RACK_URL="https://convox:<api-token>@gateway.example.com"
convox apps  # Uses standard convox CLI directly

# For developers with session token
export RACK_URL="https://convox:<session-token>@gateway.example.com"
convox apps  # Uses standard convox CLI directly
```

#### Option 2: rack-gateway CLI Wrapper (Convenience)

```bash
# Use our wrapper for easier multi-rack management
rack-gateway login staging https://gateway.example.com
rack-gateway convox apps  # Automatically sets RACK_URL with stored token

# Set up convenient shell alias
alias cx="rack-gateway convox"
cx apps
cx ps
cx deploy
```

The `rack-gateway` CLI wrapper is optional - it just provides:

- Automatic token management
- Multi-rack configuration
- Browser-based login flow
- Token expiry reminders

## CLI Usage

### Setup

```bash
# Login to a rack (sets it as current)
rack-gateway login staging https://gateway.example.com
# Opens browser for Google OAuth
# Stores configuration in ~/.config/rack-gateway/config.json
```

### Running Convox Commands

```bash
# All convox commands go through "rack-gateway convox"
rack-gateway convox apps
rack-gateway convox ps
rack-gateway convox deploy

# With the cx alias:
cx apps
cx ps
cx deploy
cx logs -f
```

### Managing Racks

```bash
# Show current rack and status
rack-gateway rack

# List all configured racks
rack-gateway racks

# Switch to a different rack
rack-gateway switch production

# With the cg alias:
cg rack
cg racks
cg switch eu-west
```

### Rack Selection

The CLI determines which rack to use in this order:

1. `--rack` flag: `rack-gateway --rack production convox apps`
2. Environment variable: `RACK_GATEWAY_RACK=production cx apps`
3. Current rack stored in `~/.config/rack-gateway/config.json`

### Generate shell completions:

```bash
# Bash
source <(./bin/rack-gateway completion bash)

# Zsh
source <(./bin/rack-gateway completion zsh)

# Fish
./bin/rack-gateway completion fish | source
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

- `~/.config/rack-gateway/config.json`: Local CLI configuration (per developer)

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
  rack-gateway-api:latest
```

### Convox

```bash
convox apps create rack-gateway
convox env set GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID -a rack-gateway
convox env set GOOGLE_CLIENT_SECRET=$GOOGLE_CLIENT_SECRET -a rack-gateway
convox deploy -a rack-gateway
```

## CloudWatch Configuration

Set log retention:

```bash
aws logs put-retention-policy \
  --log-group-name /convox/your-rack/rack-gateway \
  --retention-in-days 90
```

Create metric filters for security monitoring:

```bash
aws logs put-metric-filter \
  --log-group-name /convox/your-rack/rack-gateway \
  --filter-name rbac-denies \
  --filter-pattern '[..., rbac_decision="deny", ...]' \
  --metric-transformations \
    metricName=RBACDenies,metricNamespace=ConvoxAuth,metricValue=1
```

## Security Considerations

1. **Session Secrets**: Use strong, unique secret keys in production
2. **Domain Restriction**: Enforce Google Workspace domain
3. **TLS**: Always use HTTPS in production
4. **Rack Tokens**: Store securely, rotate regularly
5. **Audit Logs**: Monitor for anomalies and denied requests
6. **Session Management**: All sessions stored in database and revocable

## Testing

Run all tests:

```bash
task test
```

Run linters:

```bash
task lint
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
