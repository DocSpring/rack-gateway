# Deploy Approval System

This guide explains the deploy approval workflow, which adds a manual checkpoint before sensitive Convox deployment actions.

## Overview

Deploy approvals integrate with CI/CD pipelines to enforce admin review before deployments:

1. **CI tests pass** → CI pushes deploy approval request (git commit + CI metadata)
2. **Admin reviews and approves** in gateway UI (requires MFA step-up)
3. **Gateway auto-approves CI job** via provider API (if configured per-app)
4. **CI builds and deploys** → Gateway validates manifest matches approved commit
5. **Deployment actions gated** by approval throughout the lifecycle

Approvals can be disabled entirely with `RGW_SETTING_DEPLOY_APPROVALS_ENABLED=false` (useful for staging racks).

## Architecture

### Request Creation (CI/CD)

After tests pass, CI creates an approval request:

```bash
rack-gateway deploy-approval request \
  --git-commit "${CIRCLE_SHA1}" \
  --branch "${CIRCLE_BRANCH}" \
  --ci-metadata '{"workflow_id":"abc-123","pipeline_number":"6279"}' \
  --message "Deploy abc123f to production"
```

**What happens:**
- Creates pending request linked to git commit hash
- Stores CI metadata as flexible JSON for provider integration
- Target API token (CI/CD token) is automatically resolved

### Admin Approval (Web UI)

Admins review pending requests at `/.gateway/web/deploy_approval_requests`:

- View git commit hash, branch, PR link (if GitHub verification enabled)
- Click through to see code changes
- Approve with MFA step-up authentication

**When approved:**
- Request marked as `approved` with expiration time (default 15min, configurable via `RGW_SETTING_DEPLOY_APPROVAL_WINDOW_MINUTES`)
- If CI integration configured for the app: Gateway calls CI provider API to auto-approve the job
- CI pipeline unblocks and continues

### Build Validation

When CI calls `convox build`:

1. **Manifest validation**: Gateway parses submitted `convox.yml`
2. **Image tag check**: Validates `image:` entries match approved commit pattern
3. **Commit match**: Ensures git commit in image tags matches approved commit
4. **Record tracking**: Links `build_id` → `release_id` → `git_commit_hash`

If validation fails, build is rejected with clear error message.

### Deploy Enforcement

All deployment actions require active approval:

- `convox run` (with `--release`)
- `convox releases promote`

Gateway verifies:
- Approval exists for the git commit
- Approval hasn't expired
- Release ID matches approved commit

## Database Schema

The `deploy_approval_requests` table tracks the complete lifecycle:

| Column                     | Type      | Description                                                |
| -------------------------- | --------- | ---------------------------------------------------------- |
| `id`                       | bigserial | Primary key                                                |
| `public_id`                | uuid      | External UUID for API access                               |
| `app`                      | varchar   | Application name (required)                                |
| `git_commit_hash`          | varchar   | Git commit SHA (indexed, required)                         |
| `git_branch`               | varchar   | Branch name                                                |
| `pr_url`                   | text      | Pull request URL (set by GitHub verification)              |
| `ci_metadata`              | jsonb     | CI provider metadata (workflow_id, pipeline_number, etc.)  |
| `message`                  | text      | Human-readable context (required)                          |
| `status`                   | varchar   | `pending`, `approved`, `rejected`, `expired`, `deployed`   |
| `created_by_user_id`       | bigint    | User who created request                                   |
| `created_by_api_token_id`  | bigint    | API token that created request                             |
| `target_api_token_id`      | bigint    | CI/CD token that will use approval (required)              |
| `approved_by_user_id`      | bigint    | Admin who approved                                         |
| `approved_at`              | timestamp | Approval timestamp                                         |
| `approval_expires_at`      | timestamp | When approval expires                                      |
| `approval_notes`           | text      | Admin notes on approval/rejection                          |
| `rejected_by_user_id`      | bigint    | Admin who rejected                                         |
| `rejected_at`              | timestamp | Rejection timestamp                                        |
| `object_url`               | text      | Object storage URL (set during build)                      |
| `build_id`                 | varchar   | Convox build ID                                            |
| `release_id`               | varchar   | Convox release ID                                          |
| `process_ids`              | text[]    | Process IDs from exec commands                             |
| `exec_commands`            | jsonb     | Executed commands for audit trail                          |

**Note:** Each gateway is single-tenant and manages approvals for exactly one rack.

**State machine:** The database enforces strict ordering: `pending` → `approved` → `object_url` → `build_id` → `release_id` → `deployed`

## CI Integration

The gateway supports automated CI job approval after admin approval. This is configured per-app via settings.

### How It Works

1. **CI pushes request** with `ci_metadata` containing provider-specific fields
2. **Admin approves** in gateway UI
3. **Gateway reads app settings** to determine:
   - Which CI provider to use (`ci_provider`)
   - Provider-specific configuration (`ci_org_slug`, approval job names, etc.)
   - Whether auto-approval is enabled (`circleci_auto_approve_on_approval`)
4. **Gateway calls CI API** to approve the waiting job
5. **CI job unblocks** and deployment proceeds

### Configuration Layers

**1. Gateway Environment Variables (Global)**

```bash
# CircleCI API token (required for CircleCI integration)
CIRCLECI_TOKEN=your-circleci-api-token

# Default org slug (can be overridden per-app)
RGW_SETTING_DEFAULT_CI_ORG_SLUG=gh/DocSpring/docspring
```

**2. App Settings (Per-App via UI or API)**

Each app can configure:

| Setting                               | Description                                                  | Example                          |
| ------------------------------------- | ------------------------------------------------------------ | -------------------------------- |
| `ci_provider`                         | CI system to use                                             | `circleci`                       |
| `ci_org_slug`                         | Organization/repo slug for building pipeline URLs            | `gh/DocSpring/docspring`         |
| `circleci_approval_job_name`          | Name of approval job in CircleCI workflow                    | `approve_deploy_production`      |
| `circleci_auto_approve_on_approval`   | Enable automatic CircleCI job approval after admin approval  | `true`                           |

Settings can also be configured via environment variables:
```bash
# Per-app settings via environment
RGW_APP_MYAPP_SETTING_CI_PROVIDER=circleci
RGW_APP_MYAPP_SETTING_CIRCLECI_APPROVAL_JOB_NAME=approve_deploy_production
RGW_APP_MYAPP_SETTING_CIRCLECI_AUTO_APPROVE_ON_APPROVAL=true
```

### CircleCI Integration

See [CIRCLECI.md](./CIRCLECI.md) for complete CircleCI integration documentation.

**Quick summary:**

**CI Metadata Required:**
```json
{
  "workflow_id": "abc-123-def-456",
  "pipeline_number": "6279"
}
```

**What Gateway Does:**
1. Uses `workflow_id` from metadata + `approval_job_name` from app settings
2. Calls CircleCI API: `POST /workflow/{workflow_id}/approve/{job_id}`
3. CI job unblocks automatically

**Pipeline URL Building:**
- Gateway uses `ci_org_slug` + `pipeline_number` from metadata
- Builds URL: `https://app.circleci.com/pipelines/{ci_org_slug}/{pipeline_number}`
- Example: `https://app.circleci.com/pipelines/github/DocSpring/docspring/6279`

## CLI Commands

### Request Approval (CI/CD)

```bash
rack-gateway deploy-approval request \
  --app myapp \
  --git-commit abc123f \
  --branch main \
  --ci-metadata '{"workflow_id":"abc-123","pipeline_number":"6279"}' \
  --message "Deploy to production"
```

**CircleCI Example:**

```bash
rack-gateway deploy-approval request \
  --app myapp \
  --git-commit "$CIRCLE_SHA1" \
  --branch "$CIRCLE_BRANCH" \
  --ci-metadata "{\"workflow_id\":\"$CIRCLE_WORKFLOW_ID\",\"pipeline_number\":<< pipeline.number >>}" \
  --message "Deploy $CIRCLE_BRANCH@$CIRCLE_SHA1 to production"
```

### Approve (Admin)

```bash
rack-gateway deploy-approval approve <request-id> \
  --notes "Reviewed diff, LGTM"
```

Requires MFA step-up authentication.

### List Pending

```bash
rack-gateway deploy-approval list --status pending
```

### Wait for Approval (CI/CD)

```bash
rack-gateway deploy-approval request \
  --git-commit abc123f \
  --message "Deploy" \
  --wait \
  --timeout 20m
```

Blocks until approved/rejected or timeout reached.

## Permissions & RBAC

### Scopes

- `gateway:deploy-approval-request:create` – Create approval requests (CI/CD tokens)
- `gateway:deploy-approval-request:approve` – Approve/reject requests (admin only, requires MFA)
- `convox:deploy:deploy_with_approval` – Perform deployment when approval exists

### Permission Model

The `convox:deploy:deploy_with_approval` permission grants access to ALL deployment actions when an active approval exists:

- Build creation (`convox:build:create`)
- Object upload (`convox:object:create`)
- Release creation (`convox:release:create`)
- Release promotion (`convox:release:promote`)
- Process start (`convox:process:start`)
- Process exec (`convox:process:exec`)
- Process terminate (`convox:process:terminate`)

**Role assignments:**
- **CI/CD tokens:** Get `convox:deploy:deploy_with_approval` (require approval)
- **Human users:** Get direct permissions (no approval required)

## Configuration

Deploy approvals can be configured via the Settings UI or environment variables:

| Setting                                        | Default | Description                                 |
| ---------------------------------------------- | ------- | ------------------------------------------- |
| `RGW_SETTING_DEPLOY_APPROVALS_ENABLED`        | `true`  | Enable/disable approval checks globally     |
| `RGW_SETTING_DEPLOY_APPROVAL_WINDOW_MINUTES`  | `15`    | How long approvals remain valid (minutes)   |

These settings can also be managed in the web UI at `/.gateway/web/settings` under "Deploy Approvals".

## Example CI/CD Flow (CircleCI)

### 1. `.circleci/config.yml`

```yaml
version: 2.1

workflows:
  deploy:
    jobs:
      - test
      - build:
          requires:
            - test

      # Request approval from gateway
      - request_approval:
          requires:
            - build

      # CircleCI approval job (auto-approved by gateway)
      - approve_deploy_production:
          type: approval
          requires:
            - request_approval

      # Deploy after approval
      - deploy:
          requires:
            - approve_deploy_production

jobs:
  test:
    docker:
      - image: cimg/node:18.0
    steps:
      - checkout
      - run: npm test

  build:
    docker:
      - image: cimg/node:18.0
    steps:
      - checkout
      - run: convox build --app myapp --description "Build $CIRCLE_SHA1"

  request_approval:
    docker:
      - image: your-org/rack-gateway-cli:latest
    steps:
      - run:
          name: Request deploy approval
          command: |
            rack-gateway deploy-approval request \
              --git-commit "$CIRCLE_SHA1" \
              --branch "$CIRCLE_BRANCH" \
              --ci-metadata "{\"workflow_id\":\"$CIRCLE_WORKFLOW_ID\",\"pipeline_number\":<< pipeline.number >>}" \
              --message "Deploy $CIRCLE_BRANCH@$CIRCLE_SHA1 to production"
          environment:
            RACK_GATEWAY_API_TOKEN: $RACK_GATEWAY_API_TOKEN
            RACK_GATEWAY_URL: https://gateway.example.com

  deploy:
    docker:
      - image: your-org/rack-gateway-cli:latest
    steps:
      - checkout
      - run: convox deploy --app myapp
```

### 2. Admin Approval

Admin logs into gateway UI at `https://gateway.example.com/.gateway/web/deploy_approval_requests`, reviews pending request, clicks "Approve" with MFA.

### 3. Automatic Job Approval

If app has `circleci_auto_approve_on_approval=true`, gateway automatically approves the CircleCI job via API.

### 4. Build & Deploy

CI continues with build and deploy, all gated by the approved commit.

## Manifest Validation (Defense in Depth)

The gateway validates `convox.yml` to ensure deployed images match the approved commit:

```yaml
# convox.yml
services:
  web:
    image: docspringcom/app:abc123f-amd64  # Must match approved commit
  worker:
    image: docspringcom/app:abc123f-amd64
```

**Pattern from settings** (default `.*:{{GIT_COMMIT}}`):
- `{{GIT_COMMIT}}` replaced with approved commit hash
- All service images must match this pattern
- Prevents deploying arbitrary code even with compromised CI/CD token

**Note:** This is specific to teams that build images outside Convox. Teams using `convox build` for Docker builds may not need this validation.

## Security Model

Even with a compromised CI/CD token, an attacker cannot:

- Deploy without admin approval
- Deploy different code than what was approved (manifest validation)
- Bypass pre-deploy command allowlist
- Reuse approvals across commits

**Attack requires:**
- Compromised CI/CD token AND
- Admin approval for attacker's malicious commit AND
- Image tags matching the approved commit pattern

## Local Testing

### Automated Tests

- `task web:e2e` – Playwright E2E tests including approval UI
- `task go:e2e` – CLI E2E tests with approval flow
- `task ci` – Full CI pipeline

### Manual Smoke Test

1. **Start test stack:**
   ```bash
   task docker:test:up
   ```

2. **Build CLI:**
   ```bash
   task go:build
   ```

3. **Login as admin:**
   ```bash
   ./bin/rack-gateway login test http://localhost:9447 --no-open
   ```

4. **Request approval:**
   ```bash
   ./bin/rack-gateway deploy-approval request \
     --git-commit abc123f \
     --branch main \
     --ci-metadata '{"workflow_id":"test-123","pipeline_number":"42"}' \
     --message "Test deploy"
   ```

5. **Approve in UI:**
   Visit `http://localhost:9447/.gateway/web/deploy_approval_requests`

6. **Test build validation:**
   Create test `convox.yml` with matching git commit in image tags and run build.

## Additional Configuration

For complete environment variable reference, see [CONFIGURATION.md](./CONFIGURATION.md).

For CircleCI-specific setup, see [CIRCLECI.md](./CIRCLECI.md).
