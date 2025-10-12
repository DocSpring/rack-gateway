RBAC Roles and Permissions

This gateway enforces access using a DB‑backed RBAC policy. The proxy maps incoming HTTP requests to permissions of the form `convox:<resource>:<action>`, which are then checked against the user’s roles.

Effective permissions by role

- Admin: full access

  - Permissions: `convox:*:*`

- Deployer: build, configure, and ship changes; no destructive admin

  - App lifecycle (non‑destructive): `convox:app:create`, `convox:app:update`, `convox:app:restart`
  - Builds/releases: `convox:build:create`, `convox:release:create`, `convox:release:promote`
  - Environment changes are applied by creating a new release (no separate `env:set` permission)
  - Inherits all Ops and Viewer capabilities
  - Not allowed: `convox:apps:delete`, instance/registry/system administration, gateway admin UI

- Ops: operate and debug apps; no deployments or admin

  - Processes: `convox:process:(exec|start|terminate)`
  - App restarts: `convox:app:restart`
  - Environment (read): via releases endpoints (`convox:release:list`)
  - Logs: `convox:log:read`
  - Inherits all Viewer capabilities
  - Not allowed: app create/delete, builds/releases, cert/registry/system admin

- Viewer: read‑only (no secrets exposure)
  - Apps, processes, metrics, builds (read): `convox:(app|process|build|rack):(list|get|read)`
  - Releases are NOT visible to viewers (contain secrets)
  - Logs (read): `convox:log:read`
  - No write actions

Key proxy mappings (examples)

- `GET /apps` → `convox:app:list`
- `POST /apps` → `convox:app:create`
- `PUT /apps/{name}` → `convox:app:update`
- `POST /apps/{name}/restart` → `convox:app:restart`
- `DELETE /apps/{name}` → `convox:app:delete`
- Exec/logs/process management are mapped to `convox:process:*` and `convox:logs:*` accordingly.

Notes

- API tokens are checked against their own permission list (exact or wildcard like `convox:apps:*`).
- The gateway admin UI (`/api/v1/admin/*`) is admin‑only.
