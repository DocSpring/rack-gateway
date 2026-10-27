# CLI Client - Claude Development Guide

## Overview

The `rack-gateway` CLI is a multi-rack aware wrapper around the `convox` CLI that adds authentication and authorization.

## Architecture

**CLI Client (User-installed, multi-rack aware):**

- Installed as binary at `/usr/local/bin/rack-gateway`
- Wraps the real `convox` CLI
- Stores config for multiple racks in `~/.config/rack-gateway/config.json`
- Uses `--rack` flag to switch between gateways
- Never has direct access to Convox rack tokens

## Key Features

1. **Multi-rack support**: Switch between multiple gateway instances
2. **OAuth flow**: PKCE-based browser authentication
3. **Token management**: Stores session tokens per rack
4. **Convox wrapper**: Wraps `convox` CLI commands

## Configuration

Config file location: `~/.config/rack-gateway/config.json`

Structure:

```json
{
  "current": "production",
  "gateways": {
    "production": {
      "url": "https://gateway-prod.example.com",
      "token": "session-token-here",
      "email": "user@example.com",
      "expires_at": "2025-12-31T23:59:59Z",
      "mfa_preference": "webauthn",
      "notification_sound": "default",
      "sound_volume": 0.8
    },
    "staging": {
      "url": "https://gateway-staging.example.com",
      "token": "session-token-here"
    },
    "dev": {
      "url": "http://localhost:8447"
    }
  },
  "mfa_preference": "webauthn",
  "notification_sound": "default",
  "sound_volume": 0.6,
  "touch_notifications": true,
  "all_racks_exclude": ["dev", "Dev"]
}
```

`SaveConfig` keeps the directory at `0700` and the file at `0600` (it takes away group and other permissions from
an existing directory) and replaces the file atomically: it writes a `0600` temp file in the same directory, syncs it
and renames it over `config.json`. Commands change the config only through `updateConfig` (`config_lock.go`): it
holds an exclusive lock on `config.lock` while it reads the latest file, applies the change and saves, so two
commands running at once never lose each other's changes. Never write the config any other way. Login saves
nothing until it succeeds, then saves the rack's gateway and session in one change (`SaveLogin`), so a mistyped URL
or a canceled login leaves the existing session alone.

**Configuration Fields:**

**Global Settings:**
- `current` - Currently selected rack name
- `gateways` - Map of rack configurations (see GatewayConfig below)
- `machine_id` - Unique machine identifier
- `mfa_preference` - Default MFA method: `"default"`, `"webauthn"`, `"totp"` or `"yubiotp"` (a YubiKey OTP is
  typed at the code prompt and sent like a TOTP code)
- `notification_sound` - Default notification sound: `"default"`, `"disabled"`, or path to MP3 file
- `sound_volume` - Default sound volume: 0.0 to 1.0 (default: 0.6 = 60%)
- `touch_notifications` - Show a desktop notification each time a security key touch is needed (default: `true`;
  macOS via `osascript`, Linux via `notify-send` when installed)
- `all_racks_exclude` - Array of rack names to exclude when using `--rack all`

**Per-Rack Settings (GatewayConfig):**
- `url` - Gateway API URL
- `token` - Session authentication token
- `email` - User email address
- `expires_at` - Token expiration timestamp
- `session_id` - Session identifier
- `channel` - WebSocket channel for notifications
- `device_id` - Device identifier
- `device_name` - Device name
- `mfa_verified` - Whether MFA has been verified for this session
- `mfa_preference` - Per-rack MFA method override
- `notification_sound` - Per-rack notification sound override
- `sound_volume` - Per-rack sound volume override (0.0 to 1.0)

**Using --rack all:**

Deploy approval commands support `--rack all` to operate on all configured racks:

```bash
# Wait for deploy approval on all production racks (excluding dev)
cx deploy-approval wait --rack all --commit abc123

# Approve deploy on all racks
cx deploy-approval approve --rack all --commit abc123

# List deploy approval requests from all racks
cx deploy-approval list --rack all

# Approve several apps' requests for a commit (one PIN, one touch per approval). Fails without approving
# anything if a listed app has no request on a listed rack. Without a terminal (agents) there's no Enter
# prompt, and --app plus a full 40-character --commit are required.
cx deploy-approval approve --app web,worker --commit <40-char sha> --rack us,eu
```

The `all` value expands to all configured racks in `gateways`, excluding any racks listed in `all_racks_exclude`. This is useful for avoiding local development racks when operating on production infrastructure.

Without `--commit`, deploy approval commands use the full SHA of `HEAD` (`git rev-parse HEAD`): the gateway matches
`--commit` as a prefix, so a short SHA could also match another commit's request. `wait --approve` in a terminal asks
for y/N on every request whose commit isn't exactly the full `--commit` (found by `--branch`, or by a short
`--commit`); without a terminal it needs a full `--commit` up front.

**API tokens:** `--api-token`, else `RACK_GATEWAY_API_TOKEN`, replaces the stored session for every command
(`apiTokenOverride` in `config.go`; never read the env var directly). **`RACK_GATEWAY_URL`:** with an API token it
replaces the rack's URL (the rack name is only a label).
Without an API token it must match the selected rack's configured URL (`LoadRackAuth` refuses otherwise), so a
stored session token is only ever sent to the gateway that issued it. `--rack all` and rack lists are refused while
it is set.

## Commands

### Login

```bash
rack-gateway login <rack-name> <gateway-url>
```

Opens browser for OAuth flow, stores session token in config.

### Logout

```bash
rack-gateway logout
```

Ends the session on the gateway (`POST /api/v1/auth/cli/logout`), then removes the rack from config.
`--local` skips the gateway call.

### Convox Commands

```bash
rack-gateway convox <any-convox-command>
```

Proxies command to Convox API through the gateway with authentication.

### Update (`internal/cli/update`)

```bash
rack-gateway update [--check] [--version vX.Y.Z]
```

Replaces the running binary (symlinks resolved) with a GitHub release, in this order, so a failed step leaves the
installed binary untouched:

1. A temporary file in the binary's directory: an unwritable directory fails before anything is downloaded, with a
   `sudo` hint, and the final rename stays on one filesystem.
2. The archive for this platform (`update.AssetName`: darwin/arm64, linux/amd64, linux/arm64), its `.sha256` and
   its `.sigstore.json`, over HTTPS from GitHub only (`isGitHubURL` also checks every redirect), with size limits.
3. The checksum, then the Sigstore bundle (`ProvenanceVerifier`): a SLSA provenance attestation for the archive's
   digest, signed by `.github/workflows/release.yml@refs/tags/<tag>` in `update.Repository` with GitHub Actions'
   OIDC issuer. The trusted root comes from the public Sigstore instance through TUF, with no local cache.
4. The `rack-gateway` file from the archive (a regular file only), then the new binary's hidden `build-info`
   command: it must report the release's version, and keep security key support (`webauthn.Supported`) when this
   build has it. Then the rename.

Development builds (`Version` isn't a release) refuse to update. Without `--version`, it never downgrades.
`update.Repository` defaults to `DocSpring/rack-gateway`; `scripts/build-cli-release.sh` sets it to the repository
that builds the release.

The daily check (`cli_update_check.go`): the root command's `PersistentPreRun` starts `update.Checker` in the
background when the saved result (`update-check.json` in the config dir) is a day old, and `main` prints the notice
after the command and its error, waiting at most a second for a check still running. A canceled check isn't saved,
a failed one is (so an offline machine doesn't retry on every command). It's skipped for development builds, when
stderr isn't a terminal, when `CI` or `RACK_GATEWAY_NO_UPDATE_CHECK` is set, and for the commands in
`skipUpdateCheckCommands` (including cobra's completion requests).

`update/testdata` holds the v0.1.28 Linux archive's real attestation bundle and the public Sigstore trusted root
from October 2026, so a test checks the production verifier options against what `actions/attest-build-provenance`
creates. The other verifier tests sign with sigstore-go's `VirtualSigstore`.

## Testing

### E2E Tests

```bash
task go:e2e
```

Tests the complete CLI flow:

- Login via OAuth
- Token storage
- Convox command proxying
- RBAC enforcement
- Logout

**What the tests verify:**

- Admin user can run all commands
- Deployer can deploy but not delete apps
- Ops can view but not modify
- Viewer has minimal permissions

## Development

Build the CLI:

```bash
task go:build
# or
go build -o bin/rack-gateway ./cmd/rack-gateway
```

**⛔ NEVER use `go build` directly in project root** - it creates unwanted binaries. Always use `task go:build`.

## Important Notes

### Safety Warnings

**NEVER DELETE THESE DIRECTORIES:**

- `/Users/*/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP`
- Any backup directory in `/Users/*/Library/Preferences/`

The integration tests create backups of the real Convox CLI configuration to prevent data loss. If tests fail with "backup already exists", the user must manually verify and restore the backup - NEVER automatically delete it!

### OAuth Flow

`rack-gateway login` uses the RFC 8252 loopback flow (`cli_login.go`, `login_loopback.go`):

1. Generate a PKCE verifier/challenge and a random state; listen on `http://127.0.0.1:<random port>/callback`
2. `POST /api/v1/auth/cli/start` with the S256 challenge, state and loopback redirect URI; the gateway
   returns only `auth_url` (a response with `state`/`code_verifier` means an older gateway → refuse)
3. Always print the login URL, then try to open the browser (only https URLs, or http to a loopback
   identity provider when the gateway is loopback)
4. The gateway exchanges Google's code, binds the browser, runs MFA, and redirects the browser to the
   loopback listener with a single-use login code (or `error=<code>`, e.g. `canceled`)
5. Redeem the login code with the verifier at `POST /api/v1/auth/cli/complete` for a session token
6. Token stored in config file
7. The loopback listener holds the browser until then, and finally redirects it to the SPA's
   `/app/cli/auth/success` or `/app/cli/auth/error?error=<code>` page. The CLI never renders HTML itself

The browser must be on the same machine as the CLI (remote hosts: `ssh -L <port>:127.0.0.1:<port>`).
The login times out after 10 minutes. A gateway upgrade to this flow needs a matching CLI build.

### Protected env vars (`env set` / `env unset --allow-protected`)

`convox_env_protected.go` always collects MFA, and describes the change before it: one line per key, starting with
the target (`On app prod-web (rack us): set env var DATABASE_URL`), on stderr and in the PIN dialog summary, and the
app and rack in the touch prompt and notification. Values are never shown. Keys that aren't env var names are
refused before MFA, so a key can't make the dialog name another app. Without a terminal it needs an explicit
`--app`, so the command line in the PIN dialog names the app. The dialog quotes any argument with whitespace,
quotes, backslashes or anything outside printable ASCII, escaping non-ASCII characters (`pinentry.quoteArg`), so
`"X --app staging-web"` can't pass for a key and a flag.

### Running commands (`run`, `exec`)

The gateway needs an MFA proof on every exec and every process start that runs a command, whatever the step-up
window says. `convox run` (attached) starts a placeholder process (`convox.AttachedRunPlaceholder`), waits for
it, then execs the command into it. `newProcessClient` (`convox_process_client.go`) collects the command's proof
before anything goes to the rack, so the key touch comes straight after the PIN, and `processClient` sends it with
the exec (or a detached run's start) only: the placeholder start and the waits go without one. A process starts in
seconds, well within a WebAuthn challenge's five minutes or a TOTP code's 30 seconds or more. The PIN dialog
summary names the app, rack, command and justification (`commandPrompt`). `--justification` travels in `audit.JustificationHeader` (percent-encoded), which
the gateway records and doesn't forward. When `require_command_justification` is on (gateway-wide or the app's),
the CLI refuses a command without one before anything starts (it reads the global and app settings; API tokens
can't, and rely on the gateway's refusal).

`--env`, `--all-env-vars` and `--write` (`convox_process_env.go`) travel as the `cmdenv.Request` headers, checked
before anything starts (`cmdenv.CheckCommand` refuses an empty command or one starting with `NAME=value`). The CLI
reads the app's `command_env_vars` and `command_env_read_only` (`readCommandSettings`, with the justification
settings) to describe the command's env vars in the PIN dialog summary (`settings.CommandEnv.Plan`, as the gateway
decides them; `Plan.Summary` counts the app's list and names only the added and read-only ones, as Slack does) and
to warn: `envWarnings` go to `webauthn.Prompt.Warnings`, which the PIN dialog and the script
review dialog show in red (a JXA NSAlert, `macdialog`), and the terminal and `--dry-run` print as `WARNING:` lines.
The touch prompt says "can change data" when `--write` gets a read-only env var's real value.

`--script FILE` (`convox_process_command.go`) sends a local script with the command words: `scriptrun.Build` wraps
them in a fixed `sh` runner that decodes the base64 script into a temp directory and runs the words with the
file's path appended. The file is readable by every user, since the words often drop to another user (`gosu`). The gateway reads it back with `scriptrun.Parse` for the audit log, so the runner,
`SafeName` and `Parse` must stay in step. A script needs command words and `--justification`. Before any MFA
prompt, `scriptreview.Review` shows it with numbered lines, hidden characters as `<U+XXXX>` and chroma syntax
highlighting (`highlight.go`: styled spans, ANSI colors in a terminal, macOS system colors in the dialog; a lexer
that fails or takes over 2 seconds leaves the script plain): a y/N prompt on a
terminal, else a macOS JXA dialog (Cancel is the default; the texts go in as arguments, never into the JXA source),
else refused. API tokens skip the review. The dialog also takes the security key PIN (`convox_process_review.go`
`reviewPINCheck`) when the command will ask for a WebAuthn proof (not a dry run, no `--mfa-code`, `selectMFAMethod`
picks WebAuthn) and exactly one key is plugged in and has a PIN (`webauthn.SingleKeyHasPIN`). Run is then the
default. `webauthn.CheckPIN` checks each PIN without a touch (`CredentialsInfo`; a key without credential management
leaves it to the assertion), a wrong one re-shows the dialog with the retries left, and the PIN goes to
`newProcessClient`, so the assertion only needs the touch. The PIN dialog shows the script's name, line count and SHA-256, not its
text. `--dry-run` stops after the checks and the review (`runProcessCommand`): it prints the PIN dialog summary and
the command's size, with no MFA prompt and no request to the rack.

### Terminal output from the gateway

Every gateway-supplied string printed to the terminal (request IDs, statuses, token names and IDs, emails, error
bodies, login error codes) goes through `displayText` (`display.go`): control characters, C1 controls, line and
paragraph separators and bidi overrides become `?`. `RenderGatewayError` returns sanitized text, `main` prints
errors through `DisplayError` (same, but keeps newlines and tabs), and `printJSON` escapes DEL, C1 and bidi
characters as `\uXXXX`, which `encoding/json` leaves raw, so `-o json` stays valid JSON with the exact strings.

### Error Handling

- Network errors: Suggest checking gateway URL
- Auth errors: Suggest running `rack-gateway login` again
- Permission errors: Clear message about which role is required
- Token expiry: Auto-detect and prompt for re-login
- Exec and log streams: the Convox SDK (`stdsdk.Client.Websocket`) drops the response to a refused WebSocket
  handshake, so any gateway refusal of `exec`, `run` (attached) or `logs` (401, 403, 429, 400) prints only
  `websocket: bad handshake`. The refusal and its reason are in the gateway's audit log and request log. The CLI
  checks what it can before it connects: `run` and `exec` refuse a command too long for the audit log
  (`audit.CheckCommandLength`), or without a justification the app requires, before any MFA prompt or process
  start.
