RBAC Roles and Permissions

This gateway enforces access using a DB‑backed RBAC policy. The proxy maps incoming HTTP requests to permissions of the form `convox:<resource>:<action>`, which are then checked against the user’s roles.

Effective permissions by role

- Admin: full access
  - Permissions: `convox:*:*`

- Deployer: build, configure, and ship changes; no destructive admin
  - App lifecycle (non‑destructive): `convox:apps:create`, `convox:apps:update`
  - Builds/releases: `convox:builds:create`, `convox:releases:create`, `convox:releases:promote`
  - Environment changes are applied by creating a new release (no separate `env:set` permission)
  - Inherits all Ops and Viewer capabilities
  - Not allowed: `convox:apps:delete`, instance/registry/system administration, gateway admin UI

- Ops: operate and debug apps; no deployments or admin
  - Processes: `convox:ps:manage` (e.g., stop processes, exec)
  - Restarts: `convox:restart:app`
  - Environment (read): via releases endpoints (`convox:releases:list`)
  - Logs: `convox:logs:*`
  - Inherits all Viewer capabilities
  - Not allowed: app create/delete, builds/releases, cert/registry/system admin

- Viewer: read‑only (no secrets exposure)
  - Apps, processes, metrics, builds (read): `convox:(apps|ps|builds|rack):list/read`
  - Releases are NOT visible to viewers (contain secrets)
  - Logs (read): `convox:logs:read`
  - No write actions

Key proxy mappings (examples)

- `GET /apps` → `convox:apps:list`
- `POST /apps` → `convox:apps:create`
- `PUT /apps/{name}` → `convox:apps:update`
- `DELETE /apps/{name}` → `convox:apps:delete`
- Exec/logs/process management are mapped to `convox:ps:*` and `convox:logs:*` accordingly.

Notes

- API tokens are checked against their own permission list (exact or wildcard like `convox:apps:*`).
- The gateway admin UI (`/.gateway/api/admin/*`) is admin‑only.
