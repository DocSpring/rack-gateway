**Disclaimer:** Convox is a registered trademark of Convox, Inc.
This project is not affiliated with, endorsed by, or sponsored by Convox, Inc.
It’s an independent, open source, community-maintained tool that works with Convox racks.

Rack Gateway imports code from the convox/convox repository: https://github.com/convox/convox
convox/convox is licensed under the Apache License, Version 2.0: https://github.com/convox/convox/blob/master/LICENSE
See [NOTICE](./NOTICE) for details.

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

Rack Gateway can be considered a "community edition" for the hosted Convox Console.
It is an open source, self-hosted interface for managing your rack with OAuth login, RBAC,
audit logs, and secret redaction in env vars. We provide everything you need
for SOC2 compliance on your own self-hosted infrastructure.

**Rack Gateway is provided without any warranty or support. Use at your own risk.**

If you need more features or paid support, consider signing up for the official Convox Console or
inquire about an enterprise license for on-premise deployments. Convox Console provides many more
advanced features such as more authentication options, fine-grained RBAC and custom roles, support
for managing multiple racks, monitoring and alerts, workflows for CI/CD, etc.

## 📖 Documentation

The full documentation is at **[docspring.github.io/rack-gateway](https://docspring.github.io/rack-gateway/)** (source in `docs/src/content/docs/`):

- **[Quick Start](https://docspring.github.io/rack-gateway/getting-started/quick-start/)** - Run everything locally with mock services
- **[Deployment](https://docspring.github.io/rack-gateway/deployment/)** - Convox and Docker deployment, database, S3 audit anchors
- **[Environment Variables](https://docspring.github.io/rack-gateway/configuration/environment-variables/)** - Every setting
- **[CLI](https://docspring.github.io/rack-gateway/user-guide/cli/)** - Login, multi-rack use, MFA
- **[Development](https://docspring.github.io/rack-gateway/development/)** - Local setup, testing, contributing
- **[docs/CONVOX_REFERENCE.md](docs/CONVOX_REFERENCE.md)** - How Convox works
- **[AGENTS.md](AGENTS.md)** - Technical implementation details for AI agents

## Features

- **Google Workspace OAuth**: Secure authentication with domain restrictions
- **Role-Based Access Control**: Granular permissions (viewer, ops, deployer, admin)
- **Audit Logging**: Complete activity logs with automatic secret redaction
- **Single-Tenant Design**: One gateway per rack, deployed alongside Convox API
- **Multi-Rack CLI**: CLI can switch between multiple gateways using `--rack` flag
- **Session Management**: Database-backed sessions with secure token storage
- **Web UI**: Users and roles, API tokens, audit logs, settings, integrations and deploy approvals
- **MFA**: WebAuthn, TOTP, YubiKey OTP and backup codes, with step-up for sensitive actions
- **Deploy Approvals**: Manual approval workflow for CI/CD actions (build/object/release/promote)

## Quick Start

### ⚡ 5-Minute Setup (Mock Services)

Get everything running locally with mock services - no Google OAuth setup required:

```bash
# 1. Clone and install the tools
git clone https://github.com/DocSpring/rack-gateway.git
cd rack-gateway
mise install            # the pinned Go, Node, Bun and golangci-lint
scripts/dev-setup.sh    # Task, air, goimports and libfido2
brew install overmind   # or see github.com/DarthSim/overmind; task dev runs the services with it

# 2. Start everything (mise.toml already configures the mock services)
task dev
```

**🎉 You're done!** Open these URLs:

- **Web UI**: http://localhost:5223 (sign in as admin@example.com, the seeded admin; add other users on the Users page)
- **Gateway API**: http://localhost:8447
- **Mock Convox**: http://localhost:5443

### 🧪 Test the CLI

```bash
# Build the CLI, then log in (opens mock OAuth in browser)
task go:build:cli
./bin/rack-gateway login local http://localhost:8447

# Run Convox commands through the gateway
./bin/rack-gateway apps
./bin/rack-gateway ps
```

### Prerequisites

- mise - [Install mise](https://mise.jdx.dev/getting-started.html). `mise install` installs the pinned Go, Bun and
  Node versions from `mise.toml`, and mise sets the development environment variables
- Docker & Docker Compose
- libfido2 (for WebAuthn CLI support)

**Install libfido2:**

```bash
# macOS (Homebrew)
brew install libfido2

# Linux (Ubuntu/Debian)
sudo apt-add-repository ppa:yubico/stable
sudo apt-get update
sudo apt-get install libfido2-dev
```

**Install all development dependencies:**

`scripts/dev-setup.sh` (or `task dev-setup` once Task is installed) installs Task, Bun, air, goimports and
libfido2, skipping any that are already installed.

### Building

```bash
# Build everything
task build

# Individual targets
task go:build:gateway # Build gateway API server -> bin/rack-gateway-api (a testbypass build: never deploy it)
task go:build:cli     # Build gateway CLI -> bin/rack-gateway
task docker     # Build the release Docker image (test bypasses compiled out)
task test       # Run all tests
```

### Real Google OAuth Setup

For complete development setup with real Google OAuth (instead of mock), see
**[Local Setup](https://docspring.github.io/rack-gateway/development/local-setup/)**.

**Development URLs:**

- Gateway API: http://localhost:8447
- Web UI: http://localhost:5223
- Mock Convox: http://localhost:5443
- Mock OAuth: http://localhost:3345 (configurable via `MOCK_OAUTH_PORT`)
- Test Gateway API (preview stack used by E2E): http://localhost:9447
- Test Mock Convox: http://localhost:6443
- Test Mock OAuth: http://localhost:9345

Use `task docker:test:up` to boot the isolated preview-style stack on the test ports. It runs alongside the dev or preview stack while reusing the shared Postgres container.

The development environment includes a mock Google OAuth server that lets you pick one of its test accounts
(admin@example.com, deployer@example.com, ops@example.com, viewer@example.com). Only admin@example.com is a gateway
user at first (from `ADMIN_USERS`); add the others on the Users page to sign in as them.

## How It Works

Rack gateway is a secure proxy for the Convox API. It accepts session tokens (for developers) or API tokens (for CI/CD) as authentication.

### Two Ways to Use the Gateway

#### Option 1: Native Convox CLI (Direct)

```bash
# For CI/CD with API token
export RACK_URL="https://convox:<api-token>@gateway.example.com/api/v1/rack-proxy"
convox apps  # Uses standard convox CLI directly
```

The native CLI can't answer MFA prompts, so with a session token, commands that need MFA fail; use the
`rack-gateway` CLI for those.

#### Option 2: rack-gateway CLI Wrapper (Convenience)

```bash
# Use our CLI for easier multi-rack management
rack-gateway login staging https://gateway.example.com
rack-gateway apps  # Automatically proxies via the gateway

# Set up convenient shell alias
alias cg="rack-gateway"
cg apps
cg ps
cg deploy
```

The `rack-gateway` CLI is optional if you prefer the native Convox CLI. It provides:

- Automatic token management
- Multi-rack configuration
- Browser-based login flow
- MFA prompts (WebAuthn, TOTP, YubiKey OTP, backup codes) for commands that need them

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
# Convox commands are available directly
rack-gateway apps
rack-gateway ps
rack-gateway deploy

# With the cg alias:
cg apps
cg ps
cg deploy
cg logs -f
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

1. `--rack` flag: `rack-gateway --rack production apps`
2. Environment variable: `RACK_GATEWAY_RACK=production rack-gateway apps`
3. `RACK_GATEWAY_URL` (an unnamed rack, used with `RACK_GATEWAY_API_TOKEN` in CI)
4. Current rack stored in `~/.config/rack-gateway/config.json`

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

See [Environment Variables](https://docspring.github.io/rack-gateway/configuration/environment-variables/) for the full list of environment variables
and configuration options.

### Database

The gateway uses Postgres to store users, API tokens, and audit logs. Set a connection URL via `DATABASE_URL` (or equivalent `PG*` vars):

```bash
DATABASE_URL=postgres://user:pass@host:5432/dbname?sslmode=require
```

Production gateways never migrate on startup: run `./rack-gateway-api migrate` (as the admin database user) before
each release. The users in `ADMIN_USERS` are created as admins at startup; signing in never creates a user.

The CLI stores its configuration separately:

- `~/.config/rack-gateway/config.json`: Local CLI configuration (per developer)

## RBAC Model

### Roles

- **viewer**: Read-only access to apps, processes, and logs
- **ops**: Restart apps, view env (via releases), manage processes
- **deployer**: Full deployment permissions (builds/releases), non-destructive
- **admin**: Complete access to all operations
- **cicd**: Automation tokens only (not assignable to human users)

### Permissions

Format: `convox:{resource}:{action}`

Examples:

- `convox:app:list` - List applications
- `convox:app:restart` - Restart an application
- `convox:process:exec` - Exec into a running process

## Audit Logging

Every action is recorded in an append-only audit log in Postgres, chained with HMAC-SHA256 and anchored hourly to
an S3 bucket with Object Lock, and the gateway verifies the chain every hour. Each request is also logged to stdout
as JSON:

```json
{
  "ts": "2024-01-15T10:30:00Z",
  "user_email": "user@your-domain.com",
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

Sensitive data is redacted before it is logged:

- Environment variable values, unless an admin lists the key in the app's `audit_visible_env_vars` (secret and
  protected values are always redacted)
- Login codes, OAuth values and tokens in query strings

## Deployment

See [Deployment](https://docspring.github.io/rack-gateway/deployment/) for Convox and Docker. On Convox, deploy with `scripts/deploy.sh <rack>`: it
builds, runs the migrations against the new release, then promotes it (plain `convox deploy` skips migrations).

## CloudWatch Alerts

On Convox the request log goes to CloudWatch Logs (retention: `LOG_RETENTION_DAYS` in `convox.yml`). Refused
requests (401, 403 or 429, including failed sign-ins) have `rbac_decision` `deny`, so they can feed a metric
filter:

```bash
aws logs put-metric-filter \
  --log-group-name <the app's log group> \
  --filter-name rbac-denies \
  --filter-pattern '{ $.rbac_decision = "deny" }' \
  --metric-transformations \
    metricName=RBACDenies,metricNamespace=RackGateway,metricValue=1
```

## Security Considerations

Rack Gateway was built to meet SOC 2 requirements for production infrastructure.

**We highly recommend setting up Tailscale for secure rack access, so that neither the Convox API nor
the Rack Gateway API are exposed to the public internet.**

1. **Session Secrets**: Use strong, unique secret keys in production
2. **Domain Restriction**: Enforce Google Workspace domain
3. **TLS**: Always use HTTPS in production
4. **Rack Tokens**: Store securely, rotate regularly
5. **Audit Logs**: Monitor for anomalies and denied requests
6. **Session Management**: All sessions stored in database and revocable

To report a vulnerability, see [SECURITY.md](./SECURITY.md). Please don't open a public issue.

## Testing

```bash
task go:test:unit   # Go unit tests
task web:test       # Web unit tests
task web:e2e        # Playwright E2E tests
task lint:fix       # Linters, with auto-fix
```

CI runs the full suite (`task ci`) on every push. See [Testing](https://docspring.github.io/rack-gateway/development/testing/).

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
4. Run `task lint:fix` and the tests for what you changed
5. Submit pull request

See [Contributing](https://docspring.github.io/rack-gateway/development/contributing/).

## Support

For issues or questions:

- Create an issue on GitHub

DocSpring, Inc. may be able to provide basic paid support for Rack Gateway. Contact us at [support@docspring.com](mailto:support@docspring.com) for more information.

If your support needs are more complex, please consider using the official Convox Console. Convox provides both a hosted console and an enterprise license for on-premise deployments. See the [Convox](https://convox.com) website for more information.

## Releasing

**Version Management**: The project version is stored in `web/package.json`.

To create a new release:

```bash
# 1. Bump the version (also pins convox.yml to docker.io/docspringcom/rack-gateway:v1.0.1)
./scripts/bump-version.sh patch  # or minor, major

# 2. Commit and push the version bump
git commit -am "chore: bump version to v1.0.1"
git push origin main

# 3. Create and push the release tag (only repository admins can push v* tags)
./scripts/create-release-tags.sh
git push origin v1.0.1
```

The GitHub Actions release workflow (triggered only by `v*` tags):

- Checks the tag matches `web/package.json` and waits for CI and E2E to pass on the tagged commit
- Builds the Docker image for linux/amd64 without a build cache
- Pushes `docker.io/docspringcom/rack-gateway` with the version (`v1.0.1`), short commit SHA and `latest` tags
- Publishes signed build provenance attestations for the image and the CLI archive
- Creates a GitHub release with the CLI archive and its checksum

After the release completes, deploy. `convox.yml` already references the new version tag. The deploy script
builds, runs database migrations against the new release, then promotes it (production never migrates on
startup):

```bash
./scripts/deploy_all.sh        # every rack: staging -> eu -> us
./scripts/deploy.sh staging    # or one rack
```

Verify what you deployed:

```bash
gh attestation verify oci://docker.io/docspringcom/rack-gateway:v1.0.1 --repo DocSpring/rack-gateway
```

## License

Apache License, Version 2.0 - See [LICENSE](./LICENSE) for details
