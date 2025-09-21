# Screenshots

## Rack

![Screenshot of rack](./images/rack.jpg)

## Audit Logs

![Screenshot of audit logs](./images/audit-logs.jpg)

## Users

![Screenshot of users](./images/users.jpg)

## API Tokens

![Screenshot of api tokens](./images/api-tokens.jpg)

## Processes

![Screenshot of processes](./images/processes.jpg)

## Builds

![Screenshot of builds](./images/builds.jpg)

## Settings

![Screenshot of settings](./images/settings.jpg)

---

# CLI

```bash
❯ convox-gateway
Convox Gateway provides secure authenticated access to Convox racks
with SSO authentication, role-based access control, and audit logging.

To run convox commands through the gateway:
  convox-gateway convox apps
  convox-gateway convox ps
  convox-gateway convox deploy

Recommended aliases for your shell:
  alias cx="convox-gateway convox"   # cx apps, cx ps, cx deploy
  alias cg="convox-gateway"          # cg login, cg switch, cg rack

Rack management:
  convox-gateway rack                # Show current rack
  convox-gateway racks               # List all racks
  convox-gateway switch <rack>       # Switch to a different rack
  convox-gateway login <rack> <url>  # Login to a new rack

Usage:
  convox-gateway [flags]
  convox-gateway [command]

Available Commands:
  api-token   Manage API tokens for the current gateway
  completion  Generate shell completion script
  convox      Run a convox CLI command through the gateway
  env         List environment variables for an app (masked by default)
  help        Help about any command
  login       Login to a Convox rack via OAuth
  logout      Remove a rack (deletes config and token)
  rack        Show current rack and gateway information
  racks       List all configured racks
  switch      Switch to a different rack
  version     Show convox-gateway version
  web         Open the Convox Gateway web UI

Flags:
      --config string   Config directory (default "/Users/ndbroadbent/.config/convox-gateway")
  -h, --help            help for convox-gateway
      --rack string     Rack to use (overrides current rack)

Use "convox-gateway [command] --help" for more information about a command.
```
