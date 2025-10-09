# Deploy Approval Workflow

This guide explains how the deploy-approval gate works, which roles can interact with it, and the quickest way to exercise the flow locally.

## Overview

Deploy approvals add a manual checkpoint in front of sensitive Convox deployment actions. The approval flow is designed to integrate with CI/CD pipelines:

1. **CI tests pass** → CI pushes a deploy approval request with git commit metadata
2. **Admin reviews and approves** in the gateway UI or CLI (with MFA)
3. **Gateway optionally approves CircleCI job** via API (if integration configured)
4. **CI builds** → Gateway validates manifest against approved git commit
5. **CI deploys** → All deployment actions gated by the approval

Approvals can be bypassed entirely by setting `DISABLE_DEPLOY_APPROVALS=true` (intended for staging racks).

## New Architecture (Git Commit-Based)

### Push-Based Workflow

After CI tests pass, the pipeline pushes approval requests to all rack gateways:

```bash
# After tests pass in CI
rack-gateway deploy-approval request \
  --git-commit "${CIRCLE_SHA1}" \
  --branch "${CIRCLE_BRANCH}" \
  --pipeline-url "${CIRCLE_BUILD_URL}" \
  --message "Deploy abc123f to production"
```

This creates a pending approval request linked to that specific git commit.

### Admin Approval

Admins review pending requests in the UI at `/.gateway/web/deploy_approval_requests`:

- See git commit hash, branch, pipeline URL
- Click through to see diff on GitHub
- Approve with MFA

When approved:

- Request marked as `approved` with expiration time (`DEPLOY_APPROVAL_WINDOW`, default 15min)
- If CircleCI integration configured: Gateway calls CircleCI API to approve the corresponding job
- CI pipeline continues

### Build-Time Validation

When CI calls `rack-gateway build`:

1. **Manifest validation**: Gateway parses the `convox.yml` submitted with the build
2. **Image tag check**: Validates all `image:` entries match the pattern from settings (e.g., `.*:{{GIT_COMMIT}}-amd64`)
3. **Commit match**: Ensures the git commit in image tags matches the approved commit
4. **Record tracking**: Stores `build_id` → `release_id` → `git_commit_hash` in database

If validation fails, build is rejected with a clear error.

### Deploy-Time Enforcement

All deployment actions require an active approval for that git commit:

- `convox run` (with `--release`)
- `convox releases promote`

The gateway verifies:

- Approval exists for the git commit
- Approval hasn't expired
- Release ID matches the approved commit

## Permissions & Roles

### RBAC Scopes

- `gateway:deploy-approval-request:create` – Create approval requests (granted to CI/CD tokens)
- `gateway:deploy-approval-request:approve` – Approve/reject requests (admin only, requires MFA)
- `convox:deploy:deploy_with_approval` – Perform deployment actions when approval exists

### Simplified Permission Model

The `convox:deploy:deploy_with_approval` permission grants access to **all** deployment actions when an active approval exists:

- Build creation (`convox:build:create`)
- Object upload (`convox:object:create`)
- Release creation (`convox:release:create`)
- Release promotion (`convox:release:promote`)
- Process start (`convox:process:start`)
- Process exec (`convox:process:exec`)
- Process terminate (`convox:process:terminate`)

CI/CD tokens get `convox:deploy:deploy_with_approval`. Regular users get direct permissions without the approval requirement.

## Database Schema

`deploy_approval_requests` table:

| Column                                           | Notes                                                           |
| ------------------------------------------------ | --------------------------------------------------------------- |
| `id`                                             | Primary key                                                     |
| `app`                                            | App name (required)                                             |
| `created_by_user_id` / `created_by_api_token_id` | Who requested approval                                          |
| `target_api_token_id`                            | CI/CD token that will use the approval                          |
| `git_commit_hash`                                | Git commit SHA (indexed)                                        |
| `git_branch`                                     | Branch name                                                     |
| `pipeline_url`                                   | Link to CI job (GitHub Actions run, CircleCI pipeline, etc.)    |
| `message`                                        | Human-readable context                                          |
| `status`                                         | `pending`, `approved`, `rejected`, `expired`                    |
| `approval_notes`                                 | Admin notes                                                     |
| `approval_expires_at`                            | Approval expiration                                             |
| `approved_by_user_id`                            | Admin who approved                                              |
| `object_url` / `build_id` / `release_id`         | Tracking which object/build/release used this approval          |
| `ci_provider`                                    | CI provider: `circleci`, `github`, `buildkite`, `jenkins`, etc. |
| `ci_metadata`                                    | JSONB - provider-specific integration data                      |

**Note:** Each gateway is single-tenant and manages approvals for exactly one rack.

## CLI Commands

### Request Approval (CI/CD)

```bash
rack-gateway deploy-approval request \
  --app myapp \
  --git-commit abc123f \
  --branch main \
  --pipeline-url https://app.circleci.com/pipelines/... \
  --message "Deploy to production"
```

### Approve (Admin)

```bash
rack-gateway deploy-approval approve <request-id> \
  --notes "Reviewed diff, LGTM" \
  --mfa-code 123456
```

### List Pending

```bash
rack-gateway deploy-approval list --status pending
```

## Configuration

| Setting                    | Default             | Description                                                                        |
| -------------------------- | ------------------- | ---------------------------------------------------------------------------------- |
| `DISABLE_DEPLOY_APPROVALS` | `false`             | Skip approval checks entirely                                                      |
| `DEPLOY_APPROVAL_WINDOW`   | `15m`               | Approval validity duration                                                         |
| `ALLOWED_IMAGE_PATTERN`    | `.*:{{GIT_COMMIT}}` | Regex pattern for validating image tags ({{GIT_COMMIT}} replaced with actual hash) |
| `CI_INTEGRATION_ENABLED`   | `false`             | Enable automatic CI job approval after admin approval                              |
| `CI_INTEGRATION_CONFIG`    | (none)              | JSONB config for CI provider (API tokens, job names, etc.)                         |

## CI Integration

When enabled, the gateway automatically approves CI jobs after admin approval in the gateway UI. This is provider-agnostic and works with any CI system.

### How It Works

1. CI pushes approval request with `ci_provider` and `ci_metadata`
2. Admin approves in gateway UI
3. Gateway reads `ci_provider` and `ci_metadata`
4. Gateway calls the appropriate CI API to approve the job
5. CI job unblocks and proceeds with deployment

### Supported Providers

#### CircleCI

**ci_metadata:**

```json
{
  "workflow_id": "abc-123",
  "approval_job_name": "approve_deploy_staging"
}
```

**Configuration:**

```json
{
  "provider": "circleci",
  "api_token": "your-circleci-token",
  "org_slug": "gh/YourOrg",
  "approval_jobs": {
    "staging": "approve_deploy_staging",
    "production-us": "approve_deploy_us",
    "production-eu": "approve_deploy_eu"
  }
}
```

**API call:** `POST /workflow/{workflow_id}/approve/{job-id}`

#### GitHub Actions

**ci_metadata:**

```json
{
  "workflow_run_id": "123456",
  "environment": "production"
}
```

**Configuration:**

```json
{
  "provider": "github",
  "api_token": "ghp_your-github-token",
  "repo": "YourOrg/your-repo"
}
```

**API call:** `POST /repos/{owner}/{repo}/actions/runs/{run_id}/pending_deployments` (approve environment)

#### BuildKite

**ci_metadata:**

```json
{
  "build_number": "456",
  "job_id": "abc-123"
}
```

**Configuration:**

```json
{
  "provider": "buildkite",
  "api_token": "your-buildkite-token",
  "org_slug": "your-org",
  "pipeline": "deploy"
}
```

**API call:** `PUT /organizations/{org}/pipelines/{pipeline}/builds/{number}/jobs/{id}/unblock`

#### Jenkins

**ci_metadata:**

```json
{
  "build_number": "789",
  "job_name": "deploy-production"
}
```

**Configuration:**

```json
{
  "provider": "jenkins",
  "api_token": "your-jenkins-token",
  "base_url": "https://jenkins.example.com",
  "user": "deploy-bot"
}
```

**API call:** Custom script execution or Input API

### Setup

1. Enable CI integration in gateway settings
2. Configure `CI_INTEGRATION_CONFIG` with provider-specific settings
3. Ensure CI jobs include metadata when pushing approval requests
4. Gateway will automatically approve jobs when admin approves in UI

## Example CI/CD Flow (CircleCI)

### 1. After Tests Pass

```yaml
# .circleci/config.yml
request_deploy_approvals:
  docker:
    - image: docspringcom/ci:deploy-2025.09-amd64
  steps:
    - request_deploy_approval:
        environment: STAGING
        environment_label: staging
    - request_deploy_approval:
        environment: US
        environment_label: production-us
```

This pushes approval requests to all 3 racks.

### 2. Admin Approves

Admin logs into rack-gateway UI, sees pending request, clicks "Approve" with MFA.

### 3. CI Job Unblocks

If CI integration is configured, gateway automatically approves the deployment job via the CI provider's API.

### 4. Build with Validation

```yaml
deploy_app_staging:
  requires:
    - approve_deploy_staging # CircleCI approval job
  steps:
    - run:
        command: |
          # Manifest has git commit in image tags
          sed "s/:latest/:${CIRCLE_SHA1}-amd64/g" convox.yml > /tmp/convox.yml

          # Build validates manifest against approved commit
          convox-gateway convox build --app myapp
```

### 5. Deploy

```bash
convox-gateway convox run --release $RELEASE_ID 'rails db:migrate'
convox-gateway convox releases promote $RELEASE_ID
```

All gated by the approval for that specific git commit.

## Manifest Validation (Defense in Depth)

The gateway parses `convox.yml` and validates image tags match the approved commit:

```yaml
# convox.yml
services:
  web:
    image: docspringcom/app:abc123f-amd64 # Must match approved commit
  worker:
    image: docspringcom/app:abc123f-amd64
```

Pattern from settings (default `.*:{{GIT_COMMIT}}`):

- `{{GIT_COMMIT}}` is replaced with approved commit hash
- All service images must match this pattern
- Prevents deploying arbitrary code even with compromised CI/CD token

**Note:** This is specific to teams that build images outside Convox. Teams using `convox build` for Docker builds may not need this validation.

## Local Testing

### Automated Coverage

- `task web:e2e` – Playwright tests including approval UI
- `task go:e2e` – CLI E2E tests with approval flow
- `task ci` – Full pipeline

### Manual Smoke Test

1. **Start test stack**

   ```bash
   task docker:test:up
   ```

2. **Build CLI**

   ```bash
   task go:build:cli
   ```

3. **Login as admin**

   ```bash
   ./bin/rack-gateway login test http://localhost:9447 --no-open
   ```

4. **Request approval**

   ```bash
   ./bin/rack-gateway deploy-approval request \
     --git-commit abc123f \
     --branch main \
     --pipeline-url https://example.com \
     --message "Test deploy"
   ```

5. **Approve in UI**

   Visit `http://localhost:9447/.gateway/web/deploy_approval_requests`

6. **Test build validation**

   Create a test `convox.yml` with matching git commit in image tags and run build.

## Security Model

Even with a compromised CI/CD token, an attacker cannot:

- Deploy without admin approval
- Deploy different code than what was approved (manifest validation)
- Bypass pre-deploy command allowlist
- Reuse approvals across commits

Attack requires:

- Compromised CI/CD token AND
- Approval for attacker's malicious commit AND
- Matching image tag pattern

For additional environment variables or advanced configuration, consult [docs/CONFIGURATION.md](./CONFIGURATION.md).
