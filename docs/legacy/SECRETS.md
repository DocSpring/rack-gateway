# Managing Secrets and Env Vars

This gateway enforces secret masking at the proxy layer for every client (web UI, Convox CLI, scripts). Masking is always applied by default. To intentionally view plaintext values, you must be authorized and explicitly request secrets via the gateway.

## How masking works

- The gateway masks values for variables commonly treated as secrets (e.g., keys, passwords, tokens). It also respects an allowlist of additional secret keys via `CONVOX_SECRET_ENV_VARS` (comma‑separated key names).
- To see unmasked values, your account must have the `secrets:read` permission (assigned via RBAC), and you must pass `--unmask` to the CLI.

## Use `rack-gateway env` to request plaintext

The `rack-gateway` CLI exposes an `env` command that talks to the gateway API. Masking is applied by default; pass `--unmask` to request plaintext if you have permission.

Examples below assume you have logged in and selected a rack with:

```bash
rack-gateway login <rack> <gateway-origin>
```

### List env vars (masked by default)

```bash
# Masked output
rack-gateway env -a myapp

# Unmasked if you have permission
rack-gateway env --unmask -a myapp
```

### Get a single key

```bash
# Masked by default
rack-gateway env get DATABASE_URL -a myapp

# Unmasked (requires secrets:read permission)
rack-gateway env get DATABASE_URL --unmask -a myapp
```

### Set / unset env vars

`rack-gateway` provides aliases that delegate to the Convox CLI for writes:

```bash
# Set (delegates to: convox env set)
rack-gateway env set FOO=bar OTHER=baz -a myapp --promote

# Unset (delegates to: convox env unset)
rack-gateway env unset FOO -a myapp --promote
```

Notes:

- Writes occur via the Convox CLI, so they follow Convox semantics (release creation, optional `--promote`, etc.).
- Reads happen via the gateway, with masking applied unless `--unmask` is provided and authorized.

## Convox CLI behavior

- `convox env` (Convox CLI through the gateway): returns masked values by default, with masking enforced by the gateway. The native Convox CLI does not provide an "unmask" option via the gateway.
- To view plaintext values (when authorized), use `rack-gateway env --unmask`.

## Tips

- Avoid pasting unmasked secrets into terminals or logs. Prefer masked reads unless you explicitly need plaintext (`--unmask`).
- For scripting, parse JSON from the gateway API directly if needed; the CLI prints key=value lines for quick inspection.
- Add extra sensitive keys to `CONVOX_SECRET_ENV_VARS` to ensure they are masked by default.
