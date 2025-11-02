package convox

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/util/stringset"
)

// Command defines a known convox CLI command and its required permissions
type Command struct {
	Command      string   // e.g. "apps delete", "deploy", "env set"
	Permissions  []string // Required permissions (e.g. "convox:app:delete")
	AllowedFlags []string // Allowed flags (not including global flags)
	Description  string   // Human-readable description
}

// Global flags allowed on all commands
var globalAllowedFlags = []string{
	"app",  // --app
	"help", // --help
}

// Commands is the canonical mapping of convox CLI commands to permissions
var Commands = []Command{
	// Apps
	{
		Command:      "apps",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionList)},
		AllowedFlags: []string{},
		Description:  "List apps",
	},
	{
		Command:      "apps create",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionCreate)},
		AllowedFlags: []string{"generation", "lock"},
		Description:  "Create an app",
	},
	{
		Command:      "apps delete",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionDelete)},
		AllowedFlags: []string{},
		Description:  "Delete an app",
	},
	{
		Command:      "apps info",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get information about an app",
	},
	{
		Command:      "apps cancel",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate)},
		AllowedFlags: []string{},
		Description:  "Cancel an app update",
	},
	{
		Command:      "apps lock",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate)},
		AllowedFlags: []string{},
		Description:  "Enable termination protection",
	},
	{
		Command:      "apps unlock",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate)},
		AllowedFlags: []string{},
		Description:  "Disable termination protection",
	},
	{
		Command:      "apps params",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Display app parameters",
	},
	{
		Command:      "apps params set",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate)},
		AllowedFlags: []string{},
		Description:  "Set app parameters",
	},
	// Builds
	{
		Command: "build",
		Permissions: []string{
			rbac.Convox(rbac.ResourceBuild, rbac.ActionCreate),
			rbac.Convox(rbac.ResourceObject, rbac.ActionCreate),
		},
		AllowedFlags: []string{"description", "file", "manifest", "no-cache"},
		Description:  "Create a build",
	},
	{
		Command:      "builds",
		Permissions:  []string{rbac.Convox(rbac.ResourceBuild, rbac.ActionList)},
		AllowedFlags: []string{"limit"},
		Description:  "List builds",
	},
	{
		Command:      "builds info",
		Permissions:  []string{rbac.Convox(rbac.ResourceBuild, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get information about a build",
	},
	{
		Command:      "builds logs",
		Permissions:  []string{rbac.Convox(rbac.ResourceLog, rbac.ActionRead)},
		AllowedFlags: []string{"follow", "since"},
		Description:  "Get logs for a build",
	},
	{
		Command:      "builds export",
		Permissions:  []string{rbac.Convox(rbac.ResourceBuild, rbac.ActionRead)},
		AllowedFlags: []string{"file"},
		Description:  "Export a build",
	},
	{
		Command:      "builds import",
		Permissions:  []string{rbac.Convox(rbac.ResourceBuild, rbac.ActionImport)},
		AllowedFlags: []string{"file"},
		Description:  "Import a build",
	},
	// Deploy (combines build + promote)
	{
		Command: "deploy",
		Permissions: []string{
			rbac.Convox(rbac.ResourceBuild, rbac.ActionCreate),
			rbac.Convox(rbac.ResourceObject, rbac.ActionCreate),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionRead),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote),
		},
		AllowedFlags: []string{"description", "file", "manifest", "no-cache", "replace", "wait"},
		Description:  "Create and promote a build",
	},
	// Releases
	{
		Command:      "releases",
		Permissions:  []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionList)},
		AllowedFlags: []string{"limit"},
		Description:  "List releases",
	},
	{
		Command:      "releases info",
		Permissions:  []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get information about a release",
	},
	{
		Command:      "releases manifest",
		Permissions:  []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get manifest for a release",
	},
	{
		Command:      "releases promote",
		Permissions:  []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote)},
		AllowedFlags: []string{"force", "idle-timeout", "increment", "timeout"},
		Description:  "Promote a release",
	},
	{
		Command: "releases rollback",
		Permissions: []string{
			rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote),
		},
		AllowedFlags: []string{},
		Description:  "Copy an old release forward and promote it",
	},
	// Processes
	{
		Command:      "ps",
		Permissions:  []string{rbac.Convox(rbac.ResourceProcess, rbac.ActionList)},
		AllowedFlags: []string{"all"},
		Description:  "List app processes",
	},
	{
		Command:      "ps info",
		Permissions:  []string{rbac.Convox(rbac.ResourceProcess, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get information about a process",
	},
	{
		Command:      "ps stop",
		Permissions:  []string{rbac.Convox(rbac.ResourceProcess, rbac.ActionTerminate)},
		AllowedFlags: []string{},
		Description:  "Stop a process",
	},
	{
		Command:      "exec",
		Permissions:  []string{rbac.Convox(rbac.ResourceProcess, rbac.ActionExec)},
		AllowedFlags: []string{"detach", "entrypoint", "privileged"},
		Description:  "Execute a command in a running process",
	},
	{
		Command:      "run",
		Permissions:  []string{rbac.Convox(rbac.ResourceProcess, rbac.ActionExec)},
		AllowedFlags: []string{"detach", "entrypoint", "privileged", "release", "service"},
		Description:  "Execute a command in a new process",
	},
	// Environment
	{
		Command:      "env",
		Permissions:  []string{rbac.Convox(rbac.ResourceEnv, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "List env vars",
	},
	{
		Command:      "env get",
		Permissions:  []string{rbac.Convox(rbac.ResourceEnv, rbac.ActionRead)},
		AllowedFlags: []string{"release", "secrets"},
		Description:  "Get an env var",
	},
	{
		Command: "env set",
		Permissions: []string{
			rbac.Convox(rbac.ResourceEnv, rbac.ActionSet),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate),
		},
		AllowedFlags: []string{"promote", "replace"},
		Description:  "Set env var(s)",
	},
	{
		Command: "env unset",
		Permissions: []string{
			rbac.Convox(rbac.ResourceEnv, rbac.ActionUnset),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate),
		},
		AllowedFlags: []string{"promote"},
		Description:  "Unset env var(s)",
	},
	{
		Command: "env edit",
		Permissions: []string{
			rbac.Convox(rbac.ResourceEnv, rbac.ActionSet),
			rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate),
		},
		AllowedFlags: []string{"promote"},
		Description:  "Edit env interactively",
	},
	// Logs
	{
		Command:      "logs",
		Permissions:  []string{rbac.Convox(rbac.ResourceLog, rbac.ActionRead)},
		AllowedFlags: []string{"filter", "follow", "since"},
		Description:  "Get logs for an app",
	},
	// Restart
	{
		Command:      "restart",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRestart)},
		AllowedFlags: []string{},
		Description:  "Restart an app",
	},
	// Scale
	{
		Command:      "scale",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate)},
		AllowedFlags: []string{"count", "cpu", "memory"},
		Description:  "Scale a service",
	},
	// Services
	{
		Command:      "services",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "List services for an app",
	},
	{
		Command:      "services restart",
		Permissions:  []string{rbac.Convox(rbac.ResourceProcess, rbac.ActionStart)},
		AllowedFlags: []string{},
		Description:  "Restart a service",
	},
	// Instances
	{
		Command:      "instances",
		Permissions:  []string{rbac.Convox(rbac.ResourceInstance, rbac.ActionList)},
		AllowedFlags: []string{},
		Description:  "List instances",
	},
	{
		Command:      "instances ssh",
		Permissions:  []string{rbac.Convox(rbac.ResourceInstance, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Run a shell on an instance",
	},
	{
		Command:      "instances terminate",
		Permissions:  []string{rbac.Convox(rbac.ResourceInstance, rbac.ActionTerminate)},
		AllowedFlags: []string{},
		Description:  "Terminate an instance",
	},
	{
		Command:      "instances keyroll",
		Permissions:  []string{rbac.Convox(rbac.ResourceInstance, rbac.ActionKeyroll)},
		AllowedFlags: []string{},
		Description:  "Roll SSH key on instances",
	},
	// Rack
	{
		Command:      "rack",
		Permissions:  []string{rbac.Convox(rbac.ResourceRack, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get information about the rack",
	},
	{
		Command:      "rack logs",
		Permissions:  []string{rbac.Convox(rbac.ResourceLog, rbac.ActionRead)},
		AllowedFlags: []string{"filter", "follow", "since"},
		Description:  "Get logs for the rack",
	},
	{
		Command:      "rack ps",
		Permissions:  []string{rbac.Convox(rbac.ResourceRack, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "List rack processes",
	},
	{
		Command:      "rack params",
		Permissions:  []string{rbac.Convox(rbac.ResourceRack, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Display rack parameters",
	},
	{
		Command:      "rack params set",
		Permissions:  []string{rbac.Convox(rbac.ResourceRack, rbac.ActionUpdate)},
		AllowedFlags: []string{},
		Description:  "Set rack parameters",
	},
	{
		Command:      "rack update",
		Permissions:  []string{rbac.Convox(rbac.ResourceRack, rbac.ActionUpdate)},
		AllowedFlags: []string{"version"},
		Description:  "Update the rack",
	},
	// Resources
	{
		Command:      "resources",
		Permissions:  []string{rbac.Convox(rbac.ResourceResource, rbac.ActionList)},
		AllowedFlags: []string{},
		Description:  "List resources",
	},
	{
		Command:      "resources info",
		Permissions:  []string{rbac.Convox(rbac.ResourceResource, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get information about a resource",
	},
	{
		Command:      "resources url",
		Permissions:  []string{rbac.Convox(rbac.ResourceResource, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "Get URL for a resource",
	},
	// Balancers
	{
		Command:      "balancers",
		Permissions:  []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRead)},
		AllowedFlags: []string{},
		Description:  "List balancers for an app",
	},
	// SSL
	{
		Command:      "ssl",
		Permissions:  []string{rbac.Convox(rbac.ResourceCert, rbac.ActionList)},
		AllowedFlags: []string{},
		Description:  "List certificate associates for an app",
	},
	{
		Command:      "ssl update",
		Permissions:  []string{rbac.Convox(rbac.ResourceCert, rbac.ActionUpdate)},
		AllowedFlags: []string{},
		Description:  "Update certificate for an app",
	},
	// Certs
	{
		Command:      "certs",
		Permissions:  []string{rbac.Convox(rbac.ResourceCert, rbac.ActionList)},
		AllowedFlags: []string{},
		Description:  "List certificates",
	},
	{
		Command:      "certs delete",
		Permissions:  []string{rbac.Convox(rbac.ResourceCert, rbac.ActionDelete)},
		AllowedFlags: []string{},
		Description:  "Delete a certificate",
	},
	{
		Command:      "certs generate",
		Permissions:  []string{rbac.Convox(rbac.ResourceCert, rbac.ActionGenerate)},
		AllowedFlags: []string{"domains"},
		Description:  "Generate a certificate",
	},
	{
		Command:      "certs import",
		Permissions:  []string{rbac.Convox(rbac.ResourceCert, rbac.ActionImport)},
		AllowedFlags: []string{"chain", "public", "private"},
		Description:  "Import a certificate",
	},
	// Registries
	{
		Command:      "registries",
		Permissions:  []string{rbac.Convox(rbac.ResourceRegistry, rbac.ActionList)},
		AllowedFlags: []string{},
		Description:  "List private registries",
	},
	{
		Command:      "registries add",
		Permissions:  []string{rbac.Convox(rbac.ResourceRegistry, rbac.ActionAdd)},
		AllowedFlags: []string{"username", "password"},
		Description:  "Add a private registry",
	},
	{
		Command:      "registries remove",
		Permissions:  []string{rbac.Convox(rbac.ResourceRegistry, rbac.ActionRemove)},
		AllowedFlags: []string{},
		Description:  "Remove private registry",
	},
}

// commandMap indexes commands for fast lookup
var commandMap map[string]*Command

func init() {
	commandMap = make(map[string]*Command, len(Commands))
	for i := range Commands {
		cmd := &Commands[i]
		commandMap[cmd.Command] = cmd
	}
}

// LookupCommand returns the command spec for a given convox subcommand
func LookupCommand(command string) (*Command, bool) {
	cmd, ok := commandMap[command]
	return cmd, ok
}

// ValidateFlags checks if all provided flags are allowed for the command
func (c *Command) ValidateFlags(flags []string) error {
	allowed := make(map[string]bool)
	for _, f := range globalAllowedFlags {
		allowed[f] = true
	}
	for _, f := range c.AllowedFlags {
		allowed[f] = true
	}

	var invalid []string
	for _, flag := range flags {
		// Strip leading dashes and extract flag name
		flagName := strings.TrimLeft(flag, "-")
		// Handle --flag=value format
		if idx := strings.Index(flagName, "="); idx > 0 {
			flagName = flagName[:idx]
		}

		if !allowed[flagName] {
			invalid = append(invalid, flag)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("invalid flags for %q: %s", c.Command, strings.Join(invalid, ", "))
	}

	return nil
}

// AllCommands returns all known convox commands
func AllCommands() []Command {
	result := make([]Command, len(Commands))
	copy(result, Commands)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Command < result[j].Command
	})
	return result
}

// AllPermissions returns all permissions used by convox commands
func AllPermissions() []string {
	set := make(map[string]struct{})
	for _, cmd := range Commands {
		for _, perm := range cmd.Permissions {
			set[perm] = struct{}{}
		}
	}

	return stringset.SortedKeys(set)
}
