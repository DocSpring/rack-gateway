# Deploy Approval Workflow

This guide explains how the deploy-approval gate works, which roles can interact with it, and the quickest way to exercise the flow locally.

## Overview

Deploy approvals add a manual checkpoint in front of sensitive Convox actions (build, object upload, release promote) when a user or API token only has the `*-with-approval` permissions. The flow is deliberately simple:

1. A CI/CD bot (or human) asks the gateway for approval via `convox-gateway deploy-approval request "<message>"`.
2. The request lands in the `deploy_requests` table with status `pending` and is visible in the admin UI at **Deploy Requests**.
3. An administrator reviews the request, optionally adds notes, and either approves or rejects it.
4. When approved, the request stays valid for `DEPLOY_APPROVAL_WINDOW` (15 minutes by default). The first Convox action that requires approval (build/object/release) consumes the record and stores the relevant IDs so it cannot be reused.
5. If the requester has `--wait` enabled, the CLI command polls until the request leaves the pending state and exits with success/failure accordingly.

Approval can be bypassed entirely by setting `DISABLE_DEPLOY_APPROVALS=true` (intended for staging racks).

## Permissions & Roles

New RBAC scopes introduced with this feature:

- `gateway:deploy-request:create` – allows an actor to call the deploy-request creation endpoint (the CLI uses this under the hood).
- `gateway:deploy-request:approve` – surfaces the Deploy Requests admin page and enables approve/reject actions.
- `convox:build:create-with-approval`, `convox:object:create-with-approval`, `convox:release:create-with-approval`, `convox:release:promote-with-approval` – grant the ability to perform the action only when an approval record exists.

If a user/token already has the direct permission (for example `convox:build:create`), the gateway skips the approval lookup.

### Database schema

`deploy_requests` captures the lifecycle:

| Column                                           | Notes                                                            |
| ------------------------------------------------ | ---------------------------------------------------------------- |
| `id`                                             | Primary key, auto-generated                                      |
| `created_by_user_id` / `created_by_api_token_id` | Tracks who requested approval                                    |
| `target_api_token_id`                            | The CI/CD token that will consume the approval                   |
| `message`                                        | Human readable context supplied by the requester                 |
| `status`                                         | `pending`, `approved`, `rejected`, or `consumed`                 |
| `approval_notes`                                 | Optional reviewer notes                                          |
| `approval_expires_at`                            | Timestamp when the approval ages out                             |
| `build_id` / `object_id` / `release_id`          | Populated when each Convox action is executed under the approval |

## CLI Request Flow

```bash
# Request approval for a deploy using the authenticated API token (from CONVOX_GATEWAY_API_TOKEN or --api-token)
CONVOX_GATEWAY_API_TOKEN=... ./bin/convox-gateway deploy-approval request "Deploy 1234abcd to Production" --wait --timeout 20m
```

Flags:

- `--api-token` – optional override for the API token used to authenticate (otherwise read from `CONVOX_GATEWAY_API_TOKEN` or stored config).
- `--rack` – override the current rack if needed.
- `--wait` – blocks until the request is approved/rejected (with optional `--poll-interval` and `--timeout`).

If MFA step-up is required, the CLI automatically prompts before retrying the request.

### Pre-approving deployments

Administrators can pre-stage an approval for a CI token without waiting for the pipeline to request it:

```bash
./bin/convox-gateway deploy-approval pre-approve \
  "Deploy demo release" \
  --target-api-token-id 01234567-89ab-cdef-0123-456789abcdef \
  --mfa-code 123456
```

The command requires `gateway:deploy-request:approve`, an MFA step-up, and the public UUID of the target API token. The resulting request is inserted with status `approved` and expires after `DEPLOY_APPROVAL_WINDOW`. When the CI token next triggers a guarded Convox action, the pre-approved record is consumed automatically.

## Admin UI

Admins (and any role with `gateway:deploy-request:approve`) can review requests at:

```
/.gateway/web/deploy_requests
```

The page lists pending, approved, rejected and consumed requests, with filters on the top right. Approving or rejecting updates the status immediately and emits a toast notification.

## Configuration

| Setting                    | Default | Description                                                                  |
| -------------------------- | ------- | ---------------------------------------------------------------------------- |
| `DISABLE_DEPLOY_APPROVALS` | `false` | When `true`, the gateway skips approval checks entirely. Useful for staging. |
| `DEPLOY_APPROVAL_WINDOW`   | `15m`   | How long an approval remains valid after being granted.                      |

## Local Testing

### Automated coverage

- `task web:e2e` – Runs the Playwright suite against the dedicated **test** stack (pre-compiles the SPA). Includes UI coverage for approve/reject.
- `task go:e2e` – Exercises the CLI happy-path, including automated approval via the test database.
- `task ci` – Full pipeline (lint, unit, integration, web + CLI E2E) with approvals wired through the isolated stack.

### Manual smoke test

1. **Start the test stack**

   ```bash
   task docker:test:up
   ```

   This launches the gateway, mock OAuth, mock Convox, and Postgres using the dedicated test ports / database (`gateway_test`).

2. **Build the CLI**

   ```bash
   task go:build:cli
   ```

3. **Log in as an admin** (uses seeded credentials from the test fixtures)

   ```bash
   ./bin/convox-gateway login test http://localhost:9447 --no-open
   ```

   Open the printed URL in a browser, choose `admin@example.com`, and enter the displayed MFA code (or reuse the seeded secret `JBSWY3DPEHPK3PXP`).

4. **Submit an approval request**

   ```bash
   ./bin/convox-gateway deploy-approval request "Deploy demo release" --wait
   ```

   The command blocks until a reviewer acts.

5. **Review in the UI**

   - Visit `http://localhost:9447/.gateway/web/deploy_requests` in the browser.
   - Approve or reject the pending row. When approved, the CLI unblocks with a success message.

6. **Pre-approve via CLI (optional)**

   ```bash
   ./bin/convox-gateway deploy-approval pre-approve "Deploy demo release" --target-api-token-id <token-uuid> --mfa-code <code>
   ```

   This is useful when teeing up an approval before the pipeline runs.

7. **Trigger a guarded action** (optional)

   - Run a build via `convox-gateway convox builds create ...` using the same token to see the approval consumed.

8. **Shut down services**
   ```bash
   task docker:down
   ```

### Troubleshooting tips

- If approvals appear to expire immediately, verify the system clock inside the Docker containers and ensure `DEPLOY_APPROVAL_WINDOW` is set to a reasonable duration.
- Set `DISABLE_DEPLOY_APPROVALS=true` temporarily to confirm whether an issue is related to the gate versus general Convox access.
- Database fixtures live in `internal/gateway/db/migrations` and are reset by `task docker:reset:test` if the approval table state becomes inconsistent during manual experiments.

## CI Integration Notes

- The CI job should call `convox-gateway deploy-approval request ... --wait` early in the pipeline and proceed with build/object/release steps only after the command exits successfully. Pipelines with a pre-approved token can skip this step and go straight into guarded actions.
- Approvals are single-use: once a build/object/release is recorded, a new request must be created for the next deploy.
- When automating rollback flows, request a fresh approval to avoid failing the approval-consumption guard.

For additional environment variables or advanced configuration, consult [docs/CONFIGURATION.md](./CONFIGURATION.md).
