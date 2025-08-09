# Convox Implementation Reference

This document contains the actual implementation details of how Convox v3 works, discovered by analyzing the source code.

## Source Code References

The following Convox repositories are available for reference in `./reference/`:

- `reference/convox/` - The Convox CLI source code (github.com/convox/convox)
- `reference/convox_rack/` - The Convox Rack API server (github.com/convox/rack)
- `reference/convox_racks_terraform/` - Our Terraform configurations for self-hosted racks

## How Convox v3 Authentication Works

### Terraform Racks (Self-Hosted)

1. **Terraform State**: Stored in S3, contains sensitive outputs including the API URL with embedded credentials

   ```json
   {
     "api": {
       "sensitive": true,
       "type": "string",
       "value": "https://convox:<auth-token>@api.af6a420efa1ea995.convox.cloud"
     }
   }
   ```

2. **Authentication Format**: HTTP Basic Auth embedded in URL

   - Username: `convox` (fixed)
   - Password: The rack's API token
   - Example: `https://convox:token123@api.rack.convox.cloud`

3. **Local Configuration**:

   - Config location: `~/Library/Preferences/convox/` (macOS)
   - Linux would use: `~/.config/convox/` (XDG standard)
   - Windows: `%LOCALAPPDATA%\convox\`

4. **Current Rack Selection**: `~/Library/Preferences/convox/current`

   ```json
   { "name": "staging", "type": "terraform" }
   ```

5. **Rack Types**:
   - `terraform`: Local terraform management (runs terraform commands locally)
   - `console`: Convox Cloud managed (terraform runs on Convox servers)
   - `test`: Test/mock rack
   - Direct: When `RACK_URL` env var is set

### How Convox CLI Determines Which Rack to Use

The CLI checks in this order (from `pkg/rack/rack.go`):

1. **`RACK_URL` environment variable** - Direct connection, bypasses all config

   ```bash
   export RACK_URL="https://convox:token@api.rack.convox.cloud"
   ```

2. **`--rack` flag** - Command line argument

   ```bash
   convox --rack staging apps
   ```

3. **`CONVOX_RACK` environment variable** - Rack name

   ```bash
   export CONVOX_RACK=staging
   ```

4. **Local `.convox/rack` file** - Project-specific rack selection

5. **Global `current` setting** - From `~/Library/Preferences/convox/current`

### Console vs Terraform Racks

| Aspect           | Terraform Rack                 | Console Rack                |
| ---------------- | ------------------------------ | --------------------------- |
| State Management | Local machine runs `terraform` | Convox Cloud runs terraform |
| Credentials      | In terraform state (S3)        | In Convox Cloud             |
| Auth Storage     | No separate auth (in URL)      | Auth tokens in settings     |
| Use Case         | Self-hosted, full control      | Managed service             |

### API Communication

- **Protocol**: HTTPS with Basic Auth
- **Base Path**: Varies by operation (e.g., `/apps`, `/ps`, `/env`)
- **Auth Header**: Constructed from URL as `Authorization: Basic base64(convox:token)`
- **Response Format**: JSON

## Integration Strategy for Auth Proxy

### The Problem

- Terraform state contains the real API URL with embedded token
- Anyone with S3 access can get the token
- No individual user authentication or audit trail
- Shared credentials across all users

### The Solution

1. **Deploy auth proxy** on same rack as `convox-api-proxy.DOMAIN`

2. **Use `RACK_URL` override** to point Convox CLI to proxy:

   ```bash
   export RACK_URL="https://convox:JWT_TOKEN@convox-api-proxy.domain"
   ```

3. **Proxy accepts JWT as Basic Auth password**:

   - Username: `convox` (ignored)
   - Password: User's JWT token from OAuth login

4. **Proxy forwards to real Convox API**:
   - Validates JWT and checks RBAC
   - Strips incoming Basic Auth
   - Adds real rack's Basic Auth
   - Forwards request to actual Convox API

### Why This Works

- **No terraform state access needed** for developers
- **Standard Convox CLI works unchanged** via `RACK_URL`
- **Individual authentication** per developer
- **Complete audit trail** with user attribution
- **RBAC enforcement** before reaching real API
- **Token rotation** possible without affecting users

## CLI Wrapper Strategy

Instead of reimplementing all Convox commands, `docspring-convox` acts as a wrapper:

1. **Handles authentication**:

   ```bash
   docspring-convox login staging
   # Performs OAuth, stores JWT
   ```

2. **Wraps standard Convox CLI**:

   ```bash
   docspring-convox apps
   # Sets RACK_URL with JWT
   # Executes: convox apps
   ```

3. **Environment Setup**:
   ```go
   RACK_URL = "https://convox:{JWT}@proxy.domain"
   exec("convox", args...)
   ```

## Security Improvements

### Before (Direct Terraform Rack)

- Single shared token for entire rack
- Token visible to anyone with terraform state access
- No audit trail of who did what
- No ability to revoke individual access
- No granular permissions

### After (With Auth Proxy)

- Individual JWT tokens per user
- Real rack token never exposed to users
- Complete audit log with user email
- 30-day token expiry with rotation
- RBAC with roles (viewer, ops, deployer, admin)
- Ability to revoke individual access

## Implementation Notes

### Proxy Must Handle

1. **Accept Basic Auth**: Extract JWT from password field
2. **Validate JWT**: Check signature and expiry
3. **Check RBAC**: Verify user has permission for request
4. **Transform Auth**: Replace with real rack's Basic Auth
5. **Audit Log**: Record user, action, result
6. **Forward Request**: Proxy to actual Convox API

### Blocked Operations

For security, the proxy should block rack management operations:

- `convox rack update`
- `convox rack uninstall`
- `convox rack params set`
- Any terraform state modifications

These should only be done by infrastructure team with direct terraform access.

## Testing the Integration

1. **Set up proxy** on Convox rack
2. **Create test user** in RBAC system
3. **Login via OAuth**:
   ```bash
   docspring-convox login staging
   ```
4. **Test standard commands**:
   ```bash
   export RACK_URL="https://convox:$JWT@proxy.domain"
   convox apps
   convox ps -a myapp
   convox logs -a myapp
   ```
5. **Verify audit logs** show user attribution
6. **Test RBAC** blocks unauthorized operations

## Convox CLI Commands

```
$ convox help
convox api get                         query the rack api
convox apps                            list apps
convox apps cancel                     cancel an app update
convox apps create                     create an app
convox apps delete                     delete an app
convox apps export                     export an app
convox apps import                     import an app
convox apps info                       get information about an app
convox apps lock                       enable termination protection
convox apps params                     display app parameters
convox apps params set                 set app parameters
convox apps unlock                     disable termination protection
convox balancers                       list balancers for an app
convox build                           create a build
convox builds                          list builds
convox builds export                   export a build
convox builds import                   import a build
convox builds info                     get information about a build
convox builds logs                     get logs for a build
convox certs                           list certificates
convox certs delete                    delete a certificate
convox certs generate                  generate a certificate
convox certs import                    import a certificate
convox certs renew                     renew a certificate
convox config get                      get the config
convox config set                      set the config
convox configs                         list of app configs
convox cp                              copy files
convox deploy                          create and promote a build
convox env                             list env vars
convox env edit                        edit env interactively
convox env get                         get an env var
convox env set                         set env var(s)
convox env unset                       unset env var(s)
convox exec                            execute a command in a running process
convox help                            list commands
convox instances                       list instances
convox instances keyroll               roll ssh key on instances
convox instances ssh                   run a shell on an instance
convox instances terminate             terminate an instance
convox letsencrypt dns route53 add     configure letsencrypt dns route53 solver
convox letsencrypt dns route53 delete  delete letsencrypt dns route53 solver
convox letsencrypt dns route53 list    list letsencrypt dns route53 solver
convox letsencrypt dns route53 role    letsencrypt dns route53 role arn
convox letsencrypt dns route53 update  update letsencrypt dns route53 solver
convox login                           authenticate with a rack
convox logs                            get logs for an app
convox proxy                           proxy a connection inside the rack
convox ps                              list app processes
convox ps info                         get information about a process
convox ps stop                         stop a process
convox rack                            get information about the rack
convox rack access                     get rack access credential
convox rack access key rotate          rotate access key
convox rack install                    install a new rack
convox rack kubeconfig                 generate kubeconfig for rack
convox rack logs                       get logs for the rack
convox rack mv                         move a rack to or from console
convox rack params                     display rack parameters
convox rack params set                 set rack parameters
convox rack ps                         list rack processes
convox rack releases                   list rack version history
convox rack runtime attach             attach runtime integration
convox rack runtimes                   list attachable runtime integrations
convox rack scale                      scale the rack
convox rack sync                       sync v2 rack API url
convox rack uninstall                  uninstall a rack
convox rack update                     update a rack
convox racks                           list available racks
convox registries                      list private registries
convox registries add                  add a private registry
convox registries remove               remove private registry
convox releases                        list releases for an app
convox releases info                   get information about a release
convox releases manifest               get manifest for a release
convox releases promote                promote a release
convox releases rollback               copy an old release forward and promote it
convox resources                       list resources
convox resources console               start a console for a resource
convox resources export                export data from a resource
convox resources import                import data to a resource
convox resources info                  get information about a resource
convox resources proxy                 proxy a local port to a resource
convox resources url                   get url for a resource
convox restart                         restart an app
convox run                             execute a command in a new process
convox runtimes                        get list of runtimes
convox scale                           scale a service
convox services                        list services for an app
convox services restart                restart a service
convox ssl                             list certificate associates for an app
convox ssl update                      update certificate for an app
convox start                           start an application for local development
convox switch                          switch current rack
convox test                            run tests
convox update                          update the cli
convox version                         display version information
convox workflows                       get list of workflows
convox workflows run                   run workflow for specified branch or commit
```
