# Settings Reference

This document describes all available settings in Rack Gateway. Settings can be configured via environment variables or stored in the database through the web UI or API.

## Settings Resolution Order

1. **Database** - Settings saved via web UI or API
2. **Environment Variable** - Settings from deployment configuration
3. **Default Value** - Hardcoded default

## Environment Variable Naming Convention

**Global settings:**

```
RGW_SETTING_{KEY}
```

**App-specific settings:**

```
RGW_APP_{NORMALIZED_APP}_SETTING_{KEY}
```

App name normalization: uppercase, convert dashes to underscores.

Examples:

- App `gateway` → `RGW_APP_GATEWAY_SETTING_PROTECTED_ENV_VARS`
- App `my-service` → `RGW_APP_MY_SERVICE_SETTING_PROTECTED_ENV_VARS`

---

## Global Settings

Settings that apply to the entire gateway instance.

| Key                           | Type | Default | Env Var                                   | Description                                                            |
| ----------------------------- | ---- | ------- | ----------------------------------------- | ---------------------------------------------------------------------- |
| `mfa_require_all_users`       | bool | `true`  | `RGW_SETTING_MFA_REQUIRE_ALL_USERS`       | Whether MFA enrollment is required for all users                       |
| `mfa_trusted_device_ttl_days` | int  | `30`    | `RGW_SETTING_MFA_TRUSTED_DEVICE_TTL_DAYS` | Number of days a trusted device remains valid                          |
| `mfa_step_up_window_minutes`  | int  | `10`    | `RGW_SETTING_MFA_STEP_UP_WINDOW_MINUTES`  | Duration of MFA step-up authentication window for sensitive operations |
| `allow_destructive_actions`   | bool | `false` | `RGW_SETTING_ALLOW_DESTRUCTIVE_ACTIONS`   | Whether destructive actions (e.g., rack resets) are permitted          |

---

## App-Specific Settings

Settings that are scoped to individual apps. Each app can have its own configuration.

| Key                                | Type              | Default    | Env Var Example                                            | Description                                                                                                                                                                                                                             |
| ---------------------------------- | ----------------- | ---------- | ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `approved_deploy_commands`         | []string          | `null`     | `RGW_APP_GATEWAY_SETTING_APPROVED_DEPLOY_COMMANDS`         | List of commands that a CI/CD token can run during an approved deploy lifecycle (e.g., database migrations, smoke tests). Comma-separated in env vars.                                                                                  |
| `protected_env_vars`               | []string          | `null`     | `RGW_APP_GATEWAY_SETTING_PROTECTED_ENV_VARS`               | Environment variables that are both secrets (values masked) and immutable. Cannot be changed via CLI or web UI. Comma-separated in env vars.                                                                                            |
| `secret_env_vars`                  | []string          | `null`     | `RGW_APP_GATEWAY_SETTING_SECRET_ENV_VARS`                  | Environment variables that should be treated as secrets (values masked) but can still be changed. Comma-separated in env vars.                                                                                                          |
| `service_image_patterns`           | map[string]string | `null`     | `RGW_APP_GATEWAY_SETTING_SERVICE_IMAGE_PATTERNS`           | Per-service regex patterns for validating Docker images in convox.yml. Validates all build commands to ensure only images matching the pattern are allowed (e.g., prebuilt images with fixed git commit tags). JSON object in env vars. |
| `github_verification`              | bool              | `true`     | `RGW_APP_GATEWAY_SETTING_GITHUB_VERIFICATION`              | Enable GitHub verification when creating deploy approval requests and for git SHAs in build commands. Skipped when GitHub token is not configured. Applies to both CI/CD token deploys and admin deploys.                               |
| `allow_deploy_from_default_branch` | bool              | `false`    | `RGW_APP_GATEWAY_SETTING_ALLOW_DEPLOY_FROM_DEFAULT_BRANCH` | Whether deployments from the default branch are allowed. When false, requires deploying from a non-default branch. Applies to deploy approval requests (branch name in request) and admin deploys (branch lookup via git commit).       |
| `default_branch`                   | string            | `"main"`   | `RGW_APP_GATEWAY_SETTING_DEFAULT_BRANCH`                   | The default branch name for the app's repository. Used by `allow_deploy_from_default_branch` setting.                                                                                                                                   |
| `require_pr_for_branch`            | bool              | `true`     | `RGW_APP_GATEWAY_SETTING_REQUIRE_PR_FOR_BRANCH`            | Whether a GitHub pull request must exist for the branch being deployed.                                                                                                                                                                 |
| `verify_git_commit_mode`           | string            | `"latest"` | `RGW_APP_GATEWAY_SETTING_VERIFY_GIT_COMMIT_MODE`           | `"branch"` (commit must exist and belong to the provided branch), `"latest"` (commit must be the latest on the provided branch).                                                                                                        |

---

## Examples

### Global Settings via Environment

```bash
# MFA configuration
RGW_SETTING_MFA_REQUIRE_ALL_USERS=true
RGW_SETTING_MFA_TRUSTED_DEVICE_TTL_DAYS=30
RGW_SETTING_MFA_STEP_UP_WINDOW_MINUTES=10

# Destructive actions
RGW_SETTING_ALLOW_DESTRUCTIVE_ACTIONS=false
```

### App Settings via Environment

```bash
# Gateway app settings
RGW_APP_GATEWAY_SETTING_PROTECTED_ENV_VARS="RACK_TOKEN"
RGW_APP_GATEWAY_SETTING_SECRET_ENV_VARS="API_KEY,WEBHOOK_SECRET"
RGW_APP_GATEWAY_SETTING_APPROVED_DEPLOY_COMMANDS="bundle exec rake db:migrate,npm run smoke-test"
RGW_APP_GATEWAY_SETTING_GITHUB_VERIFICATION=true
RGW_APP_GATEWAY_SETTING_REQUIRE_PR_FOR_BRANCH=true
RGW_APP_GATEWAY_SETTING_VERIFY_GIT_COMMIT_MODE=latest
RGW_APP_GATEWAY_SETTING_ALLOW_DEPLOY_FROM_DEFAULT_BRANCH=false
RGW_APP_GATEWAY_SETTING_DEFAULT_BRANCH=main

# my-service app settings (note: dashes converted to underscores)
RGW_APP_MY_SERVICE_SETTING_PROTECTED_ENV_VARS="API_TOKEN"
RGW_APP_MY_SERVICE_SETTING_GITHUB_VERIFICATION=false
```

### App Settings via Database/API

When settings are saved via the web UI or API, they override environment variables.

**PUT /admin/settings** (global):

```json
{
  "mfa_require_all_users": false,
  "allow_destructive_actions": true
}
```

**PUT /apps/gateway/settings** (app-specific):

```json
{
  "protected_env_vars": ["DATABASE_URL", "SECRET_KEY", "API_KEY"],
  "github_verification": true,
  "require_pr_for_branch": true,
  "verify_git_commit_mode": "latest"
}
```

To revert a setting to its environment variable or default value, set it to `null`:

```json
{
  "protected_env_vars": null
}
```

---

## Migration Notes

### Deprecated Environment Variables

The following environment variables have been replaced by the new settings system:

- ~~`MFA_REQUIRE_ALL_USERS`~~ → `RGW_SETTING_MFA_REQUIRE_ALL_USERS`
- ~~`DB_SEED_PROTECTED_ENV_VARS`~~ → `RGW_APP_{APP}_SETTING_PROTECTED_ENV_VARS` (per-app)

### Settings Migration

Since this is a greenfield project with zero active deployments, there is no migration path. Configure settings via environment variables or the web UI after deployment.

---

## API Endpoints

- `GET /admin/settings` - Get all global settings
- `PUT /admin/settings` - Update global settings
- `GET /apps/:app/settings` - Get all settings for an app
- `PUT /apps/:app/settings` - Update settings for an app

Settings are returned with source information:

```json
{
  "mfa_require_all_users": {
    "value": true,
    "source": "env",
    "env_var": "RGW_SETTING_MFA_REQUIRE_ALL_USERS"
  }
}
```

Source values:

- `"db"` - Stored in database
- `"env"` - From environment variable
- `"default"` - Hardcoded default value
