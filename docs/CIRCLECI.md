# CircleCI Integration

The Rack Gateway provides native CircleCI integration for automated deploy approvals. When configured, the gateway can automatically approve CircleCI workflow jobs after an admin approves the deployment request.

## Overview

The CircleCI integration enables a streamlined deployment workflow:

1. **CI pushes approval request** after tests pass (using API token)
2. **Admin reviews and approves** the deployment request in the gateway web UI
3. **Gateway automatically approves** the corresponding CircleCI job
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
         │    (after tests pass)
         ↓
┌─────────────────┐
│  Rack Gateway   │
│   API Token     │
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

## Environment Variables

Configure the following environment variables to enable CircleCI integration:

### Required

- **`CIRCLE_CI_API_TOKEN`** - CircleCI personal API token with workflow approval permissions
  - Generate at: https://app.circleci.com/settings/user/tokens
  - Scopes required: `write:builds`

- **`CIRCLE_CI_APPROVAL_JOB_NAME`** - Name of the CircleCI approval job in your workflow
  - Example: `approve_deploy_staging`, `approve_deploy_production`
  - Must match the job name in your `.circleci/config.yml`

### Optional

- **`CIRCLE_CI_ORG_SLUG`** - CircleCI organization slug (e.g., `gh/your-org`)
  - Used for display purposes only
  - Not currently required for API calls

## Setup Instructions

### 1. Generate CircleCI API Token

1. Visit https://app.circleci.com/settings/user/tokens
2. Click "Create New Token"
3. Name it "Rack Gateway Deploy Approvals"
4. Copy the token (you won't see it again)

### 2. Configure Environment Variables

Add to your gateway deployment configuration:

```bash
# CircleCI Integration
CIRCLE_CI_API_TOKEN=your-circleci-api-token-here
CIRCLE_CI_APPROVAL_JOB_NAME=approve_deploy_staging
CIRCLE_CI_ORG_SLUG=gh/your-org  # optional
```

For Convox deployments, add to your `convox.yml`:

```yaml
environment:
  # ... other env vars ...
  - CIRCLE_CI_API_TOKEN
  - CIRCLE_CI_APPROVAL_JOB_NAME=approve_deploy_staging
  - CIRCLE_CI_ORG_SLUG=gh/your-org
```

Then set the secret:

```bash
convox env set CIRCLE_CI_API_TOKEN=your-token-here --app rack-gateway
```

### 3. Update CircleCI Configuration

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

      # Wait for approval (auto-approved by gateway)
      - approve_deploy_staging:
          type: approval
          requires:
            - request_approval

      # Deploy after approval
      - deploy:
          requires:
            - approve_deploy_staging

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
      - image: cimg/base:stable
    steps:
      - run:
          name: Request deploy approval from Rack Gateway
          command: |
            # Install rack-gateway CLI or use curl directly
            curl -X POST https://gateway.example.com/.gateway/api/deploy-approval-requests \
              -H "Authorization: Bearer $RACK_GATEWAY_API_TOKEN" \
              -H "Content-Type: application/json" \
              -d "{
                \"git_commit_hash\": \"$CIRCLE_SHA1\",
                \"git_branch\": \"$CIRCLE_BRANCH\",
                \"pipeline_url\": \"$CIRCLE_BUILD_URL\",
                \"ci_provider\": \"circleci\",
                \"ci_metadata\": {
                  \"workflow_id\": \"$CIRCLE_WORKFLOW_ID\",
                  \"approval_job_name\": \"approve_deploy_staging\"
                },
                \"message\": \"Deploy $CIRCLE_BRANCH@$CIRCLE_SHA1 to staging\"
              }"

  deploy:
    docker:
      - image: cimg/base:stable
    steps:
      - checkout
      - run: convox deploy --app myapp
```

### 4. Verify Integration

1. Navigate to the Integrations page in the gateway web UI
2. Verify the CircleCI card shows "Connected"
3. Check that it displays:
   - API token status (connected/not connected)
   - Approval job name
   - Organization slug (if configured)

## How It Works

### Request Phase

When your CircleCI workflow requests approval:

1. **Tests pass** and build completes
2. **`request_approval` job runs** and posts to gateway API
3. **Gateway creates approval request** with status `pending`
4. **CircleCI workflow waits** at the `approve_deploy_staging` approval job

### Approval Phase

When an admin approves in the gateway:

1. **Admin clicks "Approve"** in the web UI (requires MFA step-up)
2. **Gateway marks request as `approved`**
3. **Gateway calls CircleCI API** to approve the job:
   ```
   POST /workflow/{workflow_id}/approve/{job_id}
   ```
4. **CircleCI job proceeds** automatically
5. **Workflow continues** to the deploy step

### CircleCI Metadata Requirements

The approval request must include this metadata for CircleCI integration:

```json
{
  "ci_provider": "circleci",
  "ci_metadata": {
    "workflow_id": "abc-123-def-456",
    "approval_job_name": "approve_deploy_staging"
  }
}
```

- **`workflow_id`** - CircleCI workflow ID (available as `$CIRCLE_WORKFLOW_ID`)
- **`approval_job_name`** - Must match the job name in your config

The gateway uses these to locate and approve the correct job.

## Using the CLI

If you're using the `rack-gateway` CLI in CircleCI:

```yaml
jobs:
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
              --pipeline-url "$CIRCLE_BUILD_URL" \
              --ci-provider "circleci" \
              --message "Deploy $CIRCLE_BRANCH@$CIRCLE_SHA1 to staging"
          environment:
            RACK_GATEWAY_API_TOKEN: $RACK_GATEWAY_API_TOKEN
            RACK_GATEWAY_URL: https://gateway.example.com
```

**Note**: The CLI automatically includes CircleCI metadata when `CIRCLE_WORKFLOW_ID` is detected.

## Multiple Environments

For multiple deployment environments (staging, production), use different approval job names:

```yaml
# Staging
CIRCLE_CI_APPROVAL_JOB_NAME=approve_deploy_staging

# Production
CIRCLE_CI_APPROVAL_JOB_NAME=approve_deploy_production
```

Or configure multiple approval job patterns in your CircleCI workflow:

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

## Troubleshooting

### CircleCI Integration Shows "Not Connected"

**Check**:
- `CIRCLE_CI_API_TOKEN` environment variable is set
- `CIRCLE_CI_APPROVAL_JOB_NAME` environment variable is set
- Gateway has been restarted after adding env vars

### CircleCI Job Not Auto-Approved

**Check**:
1. **Request includes metadata**: Verify the approval request includes `ci_metadata` with `workflow_id` and `approval_job_name`
2. **Job name matches**: The `approval_job_name` in metadata must exactly match the job in `.circleci/config.yml`
3. **Workflow ID is correct**: Check that `$CIRCLE_WORKFLOW_ID` is being passed correctly
4. **Gateway logs**: Check gateway logs for CircleCI API errors:
   ```bash
   convox logs --app rack-gateway | grep -i circleci
   ```

### API Token Permissions

If you see `403 Forbidden` errors in gateway logs:

1. Verify the CircleCI API token has `write:builds` scope
2. Regenerate the token if needed
3. Update `CIRCLE_CI_API_TOKEN` environment variable

### Job Already Approved

If the job was already approved manually in CircleCI, the gateway's auto-approval will fail silently. This is expected behavior.

## Security Considerations

### API Token Storage

- CircleCI API tokens are stored encrypted in the gateway database
- Never commit API tokens to version control
- Use environment variables or secrets management

### Token Permissions

The CircleCI API token can approve ANY workflow in your organization. Consider:

- Using a dedicated service account for the token
- Rotating tokens regularly
- Monitoring approval audit logs

### Approval Validation

The gateway validates that:
- The approval request exists and is `pending`
- The requesting API token has `cicd` role
- The admin approving has `admin` role and passes MFA step-up
- The CircleCI metadata is valid

## API Reference

### Approval Request Format

```json
POST /.gateway/api/deploy-approval-requests
Authorization: Bearer <api-token>
Content-Type: application/json

{
  "git_commit_hash": "abc123def456",
  "git_branch": "main",
  "pipeline_url": "https://circleci.com/gh/org/repo/123",
  "ci_provider": "circleci",
  "ci_metadata": {
    "workflow_id": "abc-123-def-456",
    "approval_job_name": "approve_deploy_staging"
  },
  "message": "Deploy main@abc123 to staging"
}
```

### CircleCI API Calls

The gateway makes the following CircleCI API calls:

1. **Get Workflow Jobs**:
   ```
   GET /api/v2/workflow/{workflow_id}/job
   ```

2. **Approve Job**:
   ```
   POST /api/v2/workflow/{workflow_id}/approve/{job_id}
   ```

## Future Enhancements

Planned features for CircleCI integration:

- [ ] Support for multiple approval job names per environment
- [ ] Automatic workflow cancellation on rejection
- [ ] CircleCI webhook integration for real-time status updates
- [ ] Build artifact validation
- [ ] Deployment status reporting back to CircleCI

## Additional Resources

- [CircleCI API Documentation](https://circleci.com/docs/api/v2/)
- [CircleCI Approval Jobs](https://circleci.com/docs/workflows/#holding-a-workflow-for-a-manual-approval)
- [CircleCI Personal API Tokens](https://circleci.com/docs/managing-api-tokens/)
- [Rack Gateway Deploy Approval System](./DEPLOY_APPROVALS.md)
