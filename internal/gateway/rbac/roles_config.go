package rbac

import "sort"

type RoleMetadata struct {
	Label       string
	Description string
}

type roleConfig struct {
	Permissions []string
	Parents     []string
}

var roleOrder = []string{"viewer", "ops", "deployer", "cicd", "admin"}

var roleMetadata = map[string]RoleMetadata{
	"viewer": {
		Label:       "Viewer",
		Description: "Read-only access to apps, builds, processes, and rack status",
	},
	"ops": {
		Label:       "Operations",
		Description: "Restart apps, manage processes, and view environments",
	},
	"deployer": {
		Label:       "Deployer",
		Description: "Full deployment permissions including env updates",
	},
	"cicd": {
		Label:       "CI/CD",
		Description: "Recommended scope for automation tokens (not assignable to human users)",
	},
	"admin": {
		Label:       "Admin",
		Description: "Complete access to all gateway operations",
	},
}

var roleConfigs = map[string]roleConfig{
	"viewer": {
		Permissions: []string{
			Convox(ResourceApp, ActionList),
			Convox(ResourceApp, ActionRead),
			Convox(ResourceProcess, ActionList),
			Convox(ResourceProcess, ActionRead),
			Convox(ResourceInstance, ActionList),
			Convox(ResourceInstance, ActionRead),
			Convox(ResourceLog, ActionRead),
			Convox(ResourceBuild, ActionList),
			Convox(ResourceBuild, ActionRead),
			Convox(ResourceRack, ActionRead),
		},
	},
	"ops": {
		Permissions: []string{
			Convox(ResourceApp, ActionRestart),
			Convox(ResourceProcess, ActionStart),
			Convox(ResourceProcess, ActionExec),
			Convox(ResourceProcess, ActionTerminate),
			Convox(ResourceRelease, ActionList),
			Convox(ResourceEnv, ActionRead),
		},
		Parents: []string{"viewer"},
	},
	"deployer": {
		Permissions: []string{
			Convox(ResourceApp, ActionRestart),
			Convox(ResourceBuild, ActionCreate),
			Convox(ResourceObject, ActionCreate),
			Convox(ResourceRelease, ActionCreate),
			Convox(ResourceRelease, ActionRead),
			Convox(ResourceRelease, ActionPromote),
			Convox(ResourceEnv, ActionRead),
			Convox(ResourceEnv, ActionSet),
			Convox(ResourceApp, ActionUpdate),
			Gateway(ResourceDeployApprovalRequest, ActionCreate),
			Gateway(ResourceDeployApprovalRequest, ActionRead),
		},
		Parents: []string{"ops"},
	},
	"cicd": {
		Permissions: []string{
			Convox(ResourceApp, ActionList),
			Convox(ResourceApp, ActionRead),
			Convox(ResourceProcess, ActionList),
			Convox(ResourceProcess, ActionRead),
			Gateway(ResourceDeployApprovalRequest, ActionCreate),
			Gateway(ResourceDeployApprovalRequest, ActionRead),
			Convox(ResourceDeploy, ActionDeployWithApproval),
			Convox(ResourceInstance, ActionList),
			Convox(ResourceInstance, ActionRead),
			Convox(ResourceRack, ActionRead),
		},
	},
	"admin": {
		Permissions: []string{
			"convox:*:*",
			"gateway:*:*",
		},
	},
}

var (
	policies               = buildPolicies(roleConfigs)
	defaultRolePermissions = buildDefaultRolePermissions(roleConfigs)
)

func buildPolicies(cfg map[string]roleConfig) [][]string {
	var out [][]string
	for _, role := range roleOrder {
		config, ok := cfg[role]
		if !ok {
			continue
		}
		perms := append([]string(nil), config.Permissions...)
		sort.Strings(perms)
		for _, perm := range perms {
			out = append(out, []string{"p", role, perm, "*"})
		}
		parents := append([]string(nil), config.Parents...)
		sort.Strings(parents)
		for _, parent := range parents {
			out = append(out, []string{"g", role, parent})
		}
	}
	return out
}

func buildDefaultRolePermissions(cfg map[string]roleConfig) map[string][]string {
	result := make(map[string][]string, len(cfg))
	cache := make(map[string]map[string]struct{}, len(cfg))
	var flatten func(role string) map[string]struct{}
	flatten = func(role string) map[string]struct{} {
		if set, ok := cache[role]; ok {
			return set
		}
		config, ok := cfg[role]
		if !ok {
			cache[role] = map[string]struct{}{}
			return cache[role]
		}
		set := make(map[string]struct{})
		for _, perm := range config.Permissions {
			set[perm] = struct{}{}
		}
		for _, parent := range config.Parents {
			for perm := range flatten(parent) {
				set[perm] = struct{}{}
			}
		}
		cache[role] = set
		return set
	}

	for _, role := range roleOrder {
		set := flatten(role)
		perms := make([]string, 0, len(set))
		for perm := range set {
			perms = append(perms, perm)
		}
		sort.Strings(perms)
		result[role] = perms
	}

	return result
}

func RoleOrder() []string {
	return append([]string(nil), roleOrder...)
}

func RoleMetadataMap() map[string]RoleMetadata {
	out := make(map[string]RoleMetadata, len(roleMetadata))
	for k, v := range roleMetadata {
		out[k] = v
	}
	return out
}

func DefaultRolePermissions() map[string][]string {
	clone := make(map[string][]string, len(defaultRolePermissions))
	for role, perms := range defaultRolePermissions {
		clone[role] = append([]string(nil), perms...)
	}
	return clone
}

func DefaultPermissionsForRole(role string) []string {
	if perms, ok := defaultRolePermissions[role]; ok {
		return append([]string(nil), perms...)
	}
	return nil
}
