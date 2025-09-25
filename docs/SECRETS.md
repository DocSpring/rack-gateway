# Managing Secrets and Env Vars

This gateway enforces secret masking at the proxy layer for every client (web UI, Convox CLI, scripts). Masking is always applied by default. To intentionally view plaintext values, you must be authorized and explicitly request secrets via the gateway.

## How masking works

- The gateway masks values for variables commonly treated as secrets (e.g., keys, passwords, tokens). It also respects an allowlist of additional secret keys via `CONVOX_SECRET_ENV_VARS` (comma‑separated key names).
- To see unmasked values, your account must have the `secrets:read` permission (assigned via RBAC), and you must pass `--secrets` to the CLI.

## Use `convox-gateway env` to request plaintext

The `convox-gateway` CLI exposes an `env` command that talks to the gateway API. Masking is applied by default; pass `--secrets` to request plaintext if you have permission.

Examples below assume you have logged in and selected a rack with:

```bash
convox-gateway login <rack> <gateway-origin>
```

### List env vars (masked by default)

```bash
# Masked output
convox-gateway env -a myapp

# Unmasked if you have permission
convox-gateway env --secrets -a myapp
```

### Get a single key

```bash
# Masked by default
convox-gateway env get DATABASE_URL -a myapp

# Unmasked (requires secrets:read permission)
convox-gateway env get DATABASE_URL --secrets -a myapp
```

### Set / unset env vars

`convox-gateway` provides aliases that delegate to the Convox CLI for writes:

```bash
# Set (delegates to: convox env set)
convox-gateway env set FOO=bar OTHER=baz -a myapp --promote

# Unset (delegates to: convox env unset)
convox-gateway env unset FOO -a myapp --promote
```

Notes:
- Writes occur via the Convox CLI, so they follow Convox semantics (release creation, optional `--promote`, etc.).
- Reads happen via the gateway, with masking applied unless `--secrets` is provided and authorized.

## Convox CLI behavior

- `convox env` (Convox CLI through the gateway): returns masked values by default, with masking enforced by the gateway. The native Convox CLI does not provide an "unmask" option via the gateway.
- To view plaintext values (when authorized), use `convox-gateway env --secrets`.

## Tips

- Avoid pasting unmasked secrets into terminals or logs. Prefer masked reads unless you explicitly need plaintext (`--secrets`).
- For scripting, parse JSON from the gateway API directly if needed; the CLI prints key=value lines for quick inspection.
- Add extra sensitive keys to `CONVOX_SECRET_ENV_VARS` to ensure they are masked by default.
