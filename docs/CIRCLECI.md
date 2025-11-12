# CircleCI Integration

The Rack Gateway provides native CircleCI integration for automated deploy approvals. When configured, the gateway automatically approves CircleCI workflow jobs after an admin approves the deployment request in the gateway UI.

## Overview

The CircleCI integration streamlines the deployment workflow:

1. **CI pushes approval request** after tests pass (using CI/CD API token)
2. **Admin reviews and approves** the deployment request in the gateway web UI (requires MFA step-up)
3. **Gateway automatically approves** the corresponding CircleCI approval job via API
4. **CircleCI continues** the workflow and deploys the approved code

This eliminates manual approval in both systems - admins only approve once in the gateway, and CircleCI jobs proceed automatically.

## Architecture

```
┌─────────────────┐
│   CircleCI      │
│   Workflow      │
└────────┬────────┘
         │
         │ 1. Request approval
         │    (with ci_metadata)
         ↓
┌─────────────────┐
│  Rack Gateway   │
│   API           │
└────────┬────────┘
         │
         │ 2. Admin approves
         │    in web UI
         ↓
┌─────────────────┐
│  Rack Gateway   │
│   Admin UI      │
└────────┬────────┘
         │
         │ 3. Auto-approve
         │    CircleCI job
         ↓
┌─────────────────┐
│   CircleCI      │
│   Approval Job  │
└────────┬────────┘
         │
         │ 4. Continue deploy
         ↓
┌─────────────────┐
│     Convox      │
│      Rack       │
└─────────────────┘
```

## Configuration

CircleCI integration requires configuration at two levels: gateway-wide settings and per-app settings.

### Gateway Configuration (Environment Variables)

Configure the following environment variables on the gateway:

```bash
# CircleCI API token with workflow approval permissions (required)
CIRCLECI_TOKEN=your-circleci-api-token-here

# Default CI org slug (optional, can be overridden per-app)
RGW_SETTING_DEFAULT_CI_ORG_SLUG=gh/DocSpring/docspring
```

**Generate CircleCI API Token:**

1. Visit https://app.circleci.com/settings/user/tokens
2. Click "Create New Token"
3. Name it "Rack Gateway Deploy Approvals"
4. Copy the token (you won't see it again)

For Convox deployments, add to your `convox.yml`:

```yaml
environment:
  # ... other env vars ...
  - CIRCLECI_TOKEN
  - RGW_SETTING_DEFAULT_CI_ORG_SLUG=gh/DocSpring/docspring
```

Then set the secret:

```bash
convox env set CIRCLECI_TOKEN=your-token-here --app rack-gateway
```

### Per-App Configuration (Settings)

Each app that uses CircleCI integration must configure these settings (via UI or environment variables):

| Setting                             | Required | Description                                                           | Example                     |
| ----------------------------------- | -------- | --------------------------------------------------------------------- | --------------------------- |
| `ci_provider`                       | Yes      | Must be set to `circleci`                                             | `circleci`                  |
| `ci_org_slug`                       | Yes      | Organization and repo slug for building pipeline URLs                 | `gh/DocSpring/docspring`    |
| `circleci_approval_job_name`        | Yes      | Name of the CircleCI approval job in your workflow                    | `approve_deploy_production` |
| `circleci_auto_approve_on_approval` | Yes      | Enable automatic CircleCI job approval when admin approves in gateway | `true`                      |

**Setting via Environment Variables:**

```bash
# Per-app configuration via environment variables
RGW_APP_MYAPP_SETTING_CI_PROVIDER=circleci
RGW_APP_MYAPP_SETTING_CI_ORG_SLUG=gh/DocSpring/docspring
RGW_APP_MYAPP_SETTING_CIRCLECI_APPROVAL_JOB_NAME=approve_deploy_production
RGW_APP_MYAPP_SETTING_CIRCLECI_AUTO_APPROVE_ON_APPROVAL=true
```

**Setting via UI:**

Navigate to the app settings page in the gateway UI and configure the CircleCI integration settings.

### CI Org Slug Format

The `ci_org_slug` setting specifies the VCS provider, organization, and repository:

**Format:** `{vcs}/{org}/{repo}`

**Examples:**

- GitHub: `gh/DocSpring/docspring`
- Bitbucket: `bb/mycompany/myrepo`

This is used to build pipeline URLs like:

```
https://app.circleci.com/pipelines/github/DocSpring/docspring/6279
```

## Tailscale Setup for CI/CD

When deploying to production, the rack-gateway is typically accessible only via Tailscale (internal networking). CircleCI jobs need to connect to Tailscale to reach the gateway API.

### Docker Image Configuration

Tailscale is installed in the CircleCI Docker image (`docspringcom/ci:deploy`). The image includes:
- Tailscale client installed at build time
- `rack-gateway` CLI for making deploy approval requests and running Convox commands

### CircleCI Context Configuration

The shared `deploy-app` context contains `TAILSCALE_OAUTH_SECRET`. Additional environment-specific contexts (`convox-staging`, `convox-eu`, `convox-us`) provide rack-specific configuration:

| Variable                | Context         | Description                                              | Example                                   |
| ----------------------- | --------------- | -------------------------------------------------------- | ----------------------------------------- |
| `TAILSCALE_OAUTH_SECRET` | `deploy-app`   | OAuth client secret for connecting CircleCI to Tailscale | `tskey-client-...`                        |
| `RACK_GATEWAY_URL`      | `convox-*`      | Tailscale hostname of the rack-gateway                   | `https://rack-gateway-staging.tail5a6e7.ts.net` |
| `RACK_GATEWAY_API_TOKEN` | `convox-*`     | API token for authenticating with rack-gateway          | Token from rack-gateway API token management |

**Creating Tailscale OAuth Client:**

The OAuth client is created via Terraform in `tailscale/main.tf` and never expires (unlike auth keys with 90-day maximum):

1. OAuth client is provisioned with `auth_keys` scope and `tag:circleci` tag
2. Retrieve the secret after Terraform apply:
   ```bash
   cd tailscale
   terraform output -raw tailscale_circleci_oauth_secret
   ```
3. Add to CircleCI `deploy-app` context as `TAILSCALE_OAUTH_SECRET`

**Setting RACK_GATEWAY_URL:**

The Tailscale hostname is provided by the Terraform module output:
- Check Terraform outputs for `tailscale_hostname`
- Format: `https://{hostname}` (e.g., `https://rack-gateway-staging.tail5a6e7.ts.net`)

**Getting RACK_GATEWAY_API_TOKEN:**

Create an API token in the rack-gateway admin UI:
1. Navigate to Settings → API Tokens
2. Click "Create Token"
3. Assign `cicd` role
4. Copy the token and add to CircleCI context

### Automatic Tailscale Connection

The `setup_tailscale` command is automatically called by all Convox commands:

- `convox_create_release` - Creates release and starts build
- `convox_pre_release` - Runs migrations and pre-release steps
- `convox_promote` - Promotes release to production

**What it does:**

1. Starts Tailscale daemon
2. Connects using `TAILSCALE_OAUTH_SECRET` from `deploy-app` context as an ephemeral, preauthorized node
3. Verifies connectivity by checking gateway health endpoint (`/api/v1/health`)

**Example CircleCI job:**

```yaml
jobs:
  convox_create_release_staging:
    docker:
      - image: docspringcom/ci:deploy-2025.09-amd64
    context:
      - deploy-app
      - convox-staging  # Contains RACK_GATEWAY_URL, RACK_GATEWAY_API_TOKEN
    steps:
      - setup_tailscale  # Automatically called by convox_create_release command
      - convox_create_release:
          convox_app: docspring
```

### Tailscale ACL Configuration

Configure Tailscale ACLs to allow CircleCI access to rack-gateway:

```json
{
  "groups": {
    "group:circleci": ["circleci@example.com"],
    "group:rack-gateway": ["gateway@example.com"]
  },
  "acls": [
    {
      "action": "accept",
      "src": ["group:circleci"],
      "dst": ["group:rack-gateway:8080"]
    }
  ]
}
```

This allows CircleCI to connect to rack-gateway on port 8080 (the internal service port).

### Troubleshooting Tailscale Connection

**Connection fails:**

1. Verify `TAILSCALE_OAUTH_SECRET` is set in `deploy-app` context and valid
2. Check Tailscale ACLs allow `tag:circleci` to reach rack-gateway (configured in `tailscale/acl.json`)
3. Verify `RACK_GATEWAY_URL` matches the Tailscale hostname from Terraform
4. Check gateway logs for connection attempts

**Health check fails:**

1. Verify gateway is running: `curl https://${RACK_GATEWAY_URL}/api/v1/health`
2. Check Tailscale status: `tailscale status` (in CircleCI logs)
3. Verify gateway is accessible from your Tailscale network

**API token authentication fails:**

1. Verify `RACK_GATEWAY_API_TOKEN` is set in CircleCI context
2. Check token hasn't expired or been revoked
3. Verify token has `cicd` role permissions

## CircleCI Workflow Configuration

Add an approval job to your `.circleci/config.yml`:

```yaml
version: 2.1

workflows:
  deploy:
    jobs:
      - test
      - build:
          requires:
            - test

      # Request approval from Rack Gateway
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
          name: Request deploy approval from Rack Gateway
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

### Key Points

1. **`request_approval` job**: Uses `rack-gateway` CLI to create approval request with CI metadata
2. **`approve_deploy_production` job**: CircleCI approval job (type: approval) that blocks the workflow
3. **Job name must match**: The approval job name (`approve_deploy_production`) must match the `circleci_approval_job_name` setting
4. **CI metadata**: Include `workflow_id` and `pipeline_number` in the `--ci-metadata` JSON

## CI Metadata Format

The gateway requires specific metadata from CircleCI to enable auto-approval:

```json
{
  "workflow_id": "$CIRCLE_WORKFLOW_ID",
  "pipeline_number": << pipeline.number >>
}
```

**Fields:**

| Field             | Source                  | Purpose                                    |
| ----------------- | ----------------------- | ------------------------------------------ |
| `workflow_id`     | `$CIRCLE_WORKFLOW_ID`   | Used to identify and approve the CI job    |
| `pipeline_number` | `<< pipeline.number >>` | Used to build the pipeline URL for display |

**Note:** `pipeline.number` uses CircleCI's pipeline parameter syntax (`<< >>`), not environment variable syntax (`$`).

## How It Works

### Request Phase

When your CircleCI workflow requests approval:

1. **Tests pass** and build completes successfully
2. **`request_approval` job runs** and posts approval request to gateway API with CI metadata
3. **Gateway creates approval request** with status `pending` and stores CI metadata as JSON
4. **CircleCI workflow waits** at the `approve_deploy_production` approval job

### Approval Phase

When an admin approves in the gateway:

1. **Admin clicks "Approve"** in the web UI (requires MFA step-up authentication)
2. **Gateway marks request as `approved`**
3. **Gateway reads app settings** to check if auto-approval is enabled
4. **If enabled, gateway calls CircleCI API** to approve the job:
   - Fetches workflow jobs: `GET /api/v2/workflow/{workflow_id}/job`
   - Finds approval job by name matching `circleci_approval_job_name` setting
   - Approves job: `POST /api/v2/workflow/{workflow_id}/approve/{job_id}`
5. **CircleCI job proceeds** automatically
6. **Workflow continues** to the deploy step

### What the Gateway Needs

To auto-approve a CircleCI job, the gateway needs:

1. **From CI metadata** (passed by CLI):

   - `workflow_id` - To identify the workflow

2. **From app settings** (configured in gateway):

   - `circleci_approval_job_name` - To find the correct job to approve

3. **From gateway config** (environment variable):
   - `CIRCLECI_TOKEN` - To authenticate with CircleCI API

## Pipeline URL Display

The gateway builds pipeline URLs for display in the web UI using:

- `ci_org_slug` setting (e.g., `gh/DocSpring/docspring`)
- `pipeline_number` from CI metadata

**Built URL:**

```
https://app.circleci.com/pipelines/{ci_org_slug}/{pipeline_number}
```

**Example:**

```
https://app.circleci.com/pipelines/github/DocSpring/docspring/6279
```

This provides a direct link to the CircleCI pipeline in the deploy approval request detail page.

## Multiple Environments

For multiple deployment environments (staging, production), configure different approval job names per app:

**Staging Gateway:**

```bash
RGW_APP_MYAPP_SETTING_CIRCLECI_APPROVAL_JOB_NAME=approve_deploy_staging
```

**Production Gateway:**

```bash
RGW_APP_MYAPP_SETTING_CIRCLECI_APPROVAL_JOB_NAME=approve_deploy_production
```

**CircleCI Workflow:**

```yaml
workflows:
  deploy:
    jobs:
      - test
      - build

      # Staging approval
      - request_approval_staging:
          requires:
            - build
      - approve_deploy_staging:
          type: approval
          requires:
            - request_approval_staging
      - deploy_staging:
          requires:
            - approve_deploy_staging

      # Production approval
      - request_approval_production:
          requires:
            - deploy_staging
      - approve_deploy_production:
          type: approval
          requires:
            - request_approval_production
      - deploy_production:
          requires:
            - approve_deploy_production
```

## Verification

### Check Integration Status

1. Navigate to the Integrations page in the gateway web UI
2. Verify the CircleCI card shows "Connected"
3. Check that it displays:
   - API token status (connected/not connected)
   - Global org slug (if configured)

### Test the Flow

1. **Push code** to trigger CircleCI workflow
2. **Check gateway UI** for pending approval request
3. **Approve in gateway** (requires MFA)
4. **Verify CircleCI job** automatically proceeds
5. **Check logs** in gateway for CircleCI API call confirmation

## Troubleshooting

### CircleCI Integration Shows "Not Connected"

**Check:**

- `CIRCLECI_TOKEN` environment variable is set on gateway
- Gateway has been restarted after adding env vars
- Token has not expired

**Verify token:**

```bash
curl -H "Circle-Token: YOUR_TOKEN" https://circleci.com/api/v2/me
```

### CircleCI Job Not Auto-Approved

**Check:**

1. **App settings configured:**

   - `ci_provider` is set to `circleci`
   - `circleci_auto_approve_on_approval` is set to `true`
   - `circleci_approval_job_name` matches job name in workflow

2. **CI metadata included:**

   - Approval request includes `ci_metadata` with `workflow_id`
   - Can verify in gateway database or API response

3. **Job name matches exactly:**

   - `circleci_approval_job_name` setting must exactly match job name in `.circleci/config.yml`
   - Case-sensitive match required

4. **Workflow ID is correct:**

   - Check that `$CIRCLE_WORKFLOW_ID` is being passed correctly in CI metadata
   - Verify in CircleCI UI or approval request details

5. **Gateway logs:**
   ```bash
   convox logs --app rack-gateway | grep -i circleci
   ```

**Common errors:**

- `"approval job 'approve_deploy_prod' not found in workflow"` - Job name mismatch
- `"403 Forbidden"` - API token lacks permissions or has expired
- `"workflow not found"` - Invalid workflow_id

### API Token Permissions

If you see `403 Forbidden` errors in gateway logs:

1. Verify the CircleCI API token has `write:builds` scope
2. Check token hasn't expired
3. Regenerate token if needed:
   - Visit https://app.circleci.com/settings/user/tokens
   - Create new token with `write:builds` scope
   - Update `CIRCLECI_TOKEN` environment variable
   - Restart gateway

### Job Already Approved

If the job was already approved manually in CircleCI before gateway processes the approval, the gateway's auto-approval will fail with an error. This is expected behavior and can be ignored.

### Wrong Pipeline URL

If the pipeline URL shown in the UI is incorrect:

**Check:**

- `ci_org_slug` setting is correct format: `{vcs}/{org}/{repo}`
- `pipeline_number` was included in CI metadata
- VCS prefix matches your VCS (`gh` for GitHub, `bb` for Bitbucket)

## Security Considerations

### API Token Storage

- CircleCI API tokens are stored as environment variables on the gateway
- Never commit API tokens to version control
- Use secrets management (e.g., Convox env, AWS Secrets Manager)

### Token Permissions

The CircleCI API token can approve ANY workflow in your organization. Consider:

- Using a dedicated service account for the token
- Rotating tokens regularly (every 90 days recommended)
- Monitoring approval audit logs for unexpected activity
- Restricting token scope to `write:builds` only

### Approval Validation

The gateway validates that:

- Approval request exists and is in `pending` status
- Requesting API token has `cicd` role with proper permissions
- Admin approving has `admin` role and passes MFA step-up
- CircleCI metadata is valid (`workflow_id` present)
- App settings are configured correctly

## Debugging

### Enable Verbose Logging

Check gateway logs for CircleCI API calls:

```bash
# Convox
convox logs --app rack-gateway --filter circleci

# Docker
docker logs rack-gateway-api-1 | grep -i circleci
```

**Look for:**

- `"INFO: Successfully approved CircleCI job ..."`
- `"ERROR: Failed to approve CircleCI job: ..."`
- `"WARN: CircleCI auto-approve enabled but no approval_job_name configured"`

### Check Approval Request

Query the approval request directly:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
  https://gateway.example.com/api/v1/deploy-approval-requests/REQUEST_ID
```

**Verify:**

- `ci_metadata` contains `workflow_id`
- `status` is `pending` before approval
- `status` changes to `approved` after approval

### Test CircleCI API Manually

Test the CircleCI API directly:

```bash
# Get workflow jobs
curl -H "Circle-Token: YOUR_TOKEN" \
  https://circleci.com/api/v2/workflow/WORKFLOW_ID/job

# Approve job (find job_id from above response)
curl -X POST \
  -H "Circle-Token: YOUR_TOKEN" \
  https://circleci.com/api/v2/workflow/WORKFLOW_ID/approve/JOB_ID
```

## Additional Resources

- [CircleCI API Documentation](https://circleci.com/docs/api/v2/)
- [CircleCI Approval Jobs](https://circleci.com/docs/workflows/#holding-a-workflow-for-a-manual-approval)
- [CircleCI Personal API Tokens](https://circleci.com/docs/managing-api_tokens/)
- [Rack Gateway Deploy Approval System](./DEPLOY_APPROVALS.md)
- [Configuration Reference](./CONFIGURATION.md)
