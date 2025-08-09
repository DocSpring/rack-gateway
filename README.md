# Convox Gateway

Enterprise gateway for self-hosted Convox racks with SSO authentication, RBAC, and audit logging.

## Features

- **Google Workspace OAuth**: Secure authentication with domain restrictions
- **Role-Based Access Control**: Granular permissions (viewer, ops, deployer, admin)
- **Audit Logging**: Complete activity logs with automatic secret redaction
- **Multi-Rack Support**: Manage staging, US, EU, and other racks
- **Session Management**: 30-day JWT sessions with secure token storage
- **Minimal Web UI**: User and role management interface

## Quick Start

### Prerequisites

- Go 1.22+
- Google OAuth application credentials
- Convox rack API tokens

### Building

```bash
# Build everything
make all

# Individual targets
make proxy   # Build gateway API server -> bin/convox-gateway-api
make cli     # Build gateway CLI -> bin/convox-gateway
make docker  # Build Docker image
make test    # Run all tests
```

### Development Setup

1. Clone the repository:

```bash
git clone https://github.com/docspring/convox-gateway.git
cd convox-gateway
```

2. Install dependencies:

```bash
go mod download
```

3. Configure Google OAuth:
   - Go to [Google Cloud Console](https://console.cloud.google.com/)
   - Create OAuth 2.0 credentials
   - Add `http://localhost:8080/v1/login/callback` to authorized redirect URIs
   - Set environment variables:

```bash
export GOOGLE_CLIENT_ID="your-client-id.apps.googleusercontent.com"
export GOOGLE_CLIENT_SECRET="your-client-secret"
export GOOGLE_ALLOWED_DOMAIN="your-domain.com"
```

4. Configure rack tokens:

```bash
export RACK_TOKEN_STAGING="your-staging-token"
export RACK_TOKEN_US="your-us-token"
export RACK_TOKEN_EU="your-eu-token"
```

5. Build the binaries:

```bash
# Build both proxy and CLI
make all

# Or build individually
make proxy  # Builds bin/convox-gateway-api
make cli    # Builds bin/convox-gateway
```

6. Run the proxy:

```bash
# Run directly for development
make dev

# Or run the built binary
./bin/convox-gateway-api
```

## CLI Usage

### Login to a rack:

```bash
./bin/convox-gateway login staging
# Opens browser for Google OAuth
# Stores token in ~/.config/convox-gateway/tokens.json
```

### Run commands on a rack:

```bash
# The CLI automatically wraps convox commands
convox-gateway apps --rack staging
convox-gateway ps web --rack staging
convox-gateway logs --rack staging

# Or set default rack via environment
export CONVOX_RACK=staging
convox-gateway apps
convox-gateway ps web
```

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

### Environment Variables

| Variable                | Description                             | Default         |
| ----------------------- | --------------------------------------- | --------------- |
| `PORT`                  | Server port                             | 8080            |
| `APP_JWT_KEY`           | JWT signing key (auto-generated in dev) | -               |
| `GOOGLE_CLIENT_ID`      | Google OAuth client ID                  | -               |
| `GOOGLE_CLIENT_SECRET`  | Google OAuth client secret              | -               |
| `GOOGLE_ALLOWED_DOMAIN` | Allowed email domain                    | your-domain.com |
| `ADMIN_USERS`           | Comma-separated admin emails            | -               |
| `RACK_URL_*`            | Convox rack API URLs                    | -               |
| `RACK_TOKEN_*`          | Convox rack API tokens                  | -               |
| `DEV_MODE`              | Enable development mode                 | false           |

### Configuration Files

- `config/policies.yaml`: RBAC policy definitions
- `config/users.yaml`: User role assignments (auto-created)
- `config/roles.yaml`: Role definitions (auto-created)
- `~/.config/convox-gateway/config.json`: Local CLI configuration (per developer)

## RBAC Model

### Roles

- **viewer**: Read-only access to apps, processes, and logs
- **ops**: Restart apps, view environments, manage processes
- **deployer**: Full deployment permissions including env vars
- **admin**: Complete access to all operations

### Permissions

Format: `convox:{resource}:{action}`

Examples:

- `convox:apps:list` - List applications
- `convox:ps:manage` - Manage processes
- `convox:env:set` - Set environment variables
- `convox:run:command` - Run commands
- `convox:restart:app` - Restart applications

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
make docker
docker run -p 8080:8080 \
  -e GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID \
  -e GOOGLE_CLIENT_SECRET=$GOOGLE_CLIENT_SECRET \
  -e RACK_TOKEN_STAGING=$RACK_TOKEN_STAGING \
  convox-gateway-api:latest
```

### Convox

```bash
convox apps create convox-gateway
convox env set GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID -a convox-gateway
convox env set GOOGLE_CLIENT_SECRET=$GOOGLE_CLIENT_SECRET -a convox-gateway
convox deploy -a convox-gateway
```

### Terraform Integration

The `terraform/` directory contains modules for:

- KMS key for secret encryption
- CloudWatch log group with 90-day retention
- SSM parameters for secure secret storage

To integrate with existing Terraform:

```hcl
module "convox_gateway" {
  source = "./convox-gateway/terraform"

  environment          = "production"
  convox_rack         = var.convox_rack
  google_client_id    = var.google_client_id
  google_client_secret = var.google_client_secret
  rack_tokens         = var.rack_tokens
  admin_users         = "admin@your-domain.com"
  domain              = "gateway.your-domain.com"
}
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
make test
```

Run linters:

```bash
make lint
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
4. Run `make test` and `make lint`
5. Submit pull request

## License

MIT License - See LICENSE file for details

## Support

For issues or questions:

- Create an issue on GitHub
- Check audit logs for debugging
- Review CloudWatch logs for errors
