package rbac

// HTTP methods including custom ones for Convox
const (
	MethodGet     = "GET"
	MethodPost    = "POST"
	MethodPut     = "PUT"
	MethodDelete  = "DELETE"
	MethodHead    = "HEAD"
	MethodOptions = "OPTIONS"
	MethodPatch   = "PATCH"
	MethodSocket  = "SOCKET" // WebSocket connections
	MethodAny     = "*"      // Wildcard - matches any HTTP method
)

// Route represents an HTTP route with method and path
type Route struct {
	Method string // One of the Method* constants above
	Path   string // e.g. "/apps/{name}"
}

// PolicyDef represents a policy definition with routes
type PolicyDef struct {
	Description string
	Inherits    string
	Routes      []Route
}

// DefaultPolicies defines the built-in RBAC policies
var DefaultPolicies = map[string]*PolicyDef{
	"viewer": {
		Description: "Read-only access to apps and resources",
		Routes: []Route{
			// App viewing
			{MethodGet, "/apps"},
			{MethodGet, "/apps/{name}"},
			{MethodGet, "/apps/{app}/metrics"},

			// Process viewing
			{MethodGet, "/apps/{app}/processes"},
			{MethodGet, "/apps/{app}/processes/{pid}"},

			// Logs viewing (WebSocket)
			{MethodSocket, "/apps/{name}/logs"},
			{MethodSocket, "/apps/{app}/builds/{id}/logs"},
			{MethodSocket, "/apps/{app}/processes/{pid}/logs"},
			{MethodSocket, "/apps/{app}/services/{service}/logs"},
			{MethodSocket, "/system/logs"},

			// Release and build info
			{MethodGet, "/apps/{app}/releases"},
			{MethodGet, "/apps/{app}/releases/{id}"},
			{MethodGet, "/apps/{app}/builds"},
			{MethodGet, "/apps/{app}/builds/{id}"},
			{MethodGet, "/apps/{app}/builds/{id}.tgz"},

			// Service and resource info
			{MethodGet, "/apps/{app}/services"},
			{MethodGet, "/apps/{app}/resources"},
			{MethodGet, "/apps/{app}/resources/{name}"},
			{MethodGet, "/apps/{app}/resources/{name}/data"},
			{MethodGet, "/apps/{app}/balancers"},
			{MethodGet, "/resources"},
			{MethodGet, "/resources/{name}"},
			{MethodOptions, "/resources"},

			// Config viewing
			{MethodGet, "/apps/{app}/configs"},
			{MethodGet, "/apps/{app}/configs/{name}"},

			// System info
			{MethodGet, "/system"},
			{MethodGet, "/system/capacity"},
			{MethodGet, "/system/metrics"},
			{MethodGet, "/system/processes"},
			{MethodGet, "/system/releases"},

			// Instance info
			{MethodGet, "/instances"},

			// Certificate info
			{MethodGet, "/certificates"},
			{MethodGet, "/letsencrypt/config"},

			// Registry info
			{MethodGet, "/registries"},
		},
	},

	"ops": {
		Description: "Operations team - can restart and debug apps",
		Inherits:    "viewer",
		Routes: []Route{
			// Process management
			{MethodDelete, "/apps/{app}/processes/{pid}"},
			{MethodPost, "/apps/{app}/services/{name}/restart"},

			// Process execution
			{MethodSocket, "/apps/{app}/processes/{pid}/exec"},
			{MethodPost, "/apps/{app}/services/{service}/processes"},

			// Debugging tools
			{MethodSocket, "/proxy/{host}/{port}"},
			{MethodSocket, "/apps/{app}/resources/{name}/console"},
			{MethodSocket, "/instances/{id}/shell"},

			// File operations (for debugging)
			{MethodGet, "/apps/{app}/processes/{pid}/files"},
			{MethodPost, "/apps/{app}/processes/{pid}/files"},
			{MethodDelete, "/apps/{app}/processes/{pid}/files"},

			// Object storage (read-only)
			{MethodHead, "/apps/{app}/objects/{key:.*}"},
			{MethodGet, "/apps/{app}/objects/{key:.*}"},
			{MethodGet, "/apps/{app}/objects"},
		},
	},

	"deployer": {
		Description: "Can deploy apps and manage configurations",
		Inherits:    "ops",
		Routes: []Route{
			// Build and deploy
			{MethodPost, "/apps/{app}/builds"},
			{MethodPost, "/apps/{app}/builds/import"},
			{MethodPut, "/apps/{app}/builds/{id}"},
			{MethodPost, "/apps/{app}/releases"},
			{MethodPost, "/apps/{app}/releases/{id}/promote"},

			// Config management
			{MethodPut, "/apps/{app}/configs/{name}"},

			// App management
			{MethodPost, "/apps"},
			{MethodPost, "/apps/{name}/cancel"},
			{MethodPut, "/apps/{name}"},
			{MethodPut, "/apps/{app}/services/{name}"},

			// Certificate management
			{MethodPost, "/certificates"},
			{MethodPost, "/certificates/generate"},
			{MethodPost, "/certificates/{id}/renew"},
			{MethodPut, "/apps/{app}/ssl/{service}/{port}"},
			{MethodPut, "/letsencrypt/config"},

			// Resource management
			{MethodPut, "/apps/{app}/resources/{name}/data"},
			{MethodPost, "/resources"},
			{MethodPost, "/resources/{name}/links"},
			{MethodPut, "/resources/{name}"},

			// Object storage (write)
			{MethodPost, "/apps/{app}/objects/{key:.*}"},
			{MethodDelete, "/apps/{app}/objects/{key:.*}"},

			// Event posting
			{MethodPost, "/events"},
		},
	},

	"admin": {
		Description: "Full admin access",
		Inherits:    "deployer",
		Routes: []Route{
			// Destructive operations
			{MethodDelete, "/apps/{name}"},
			{MethodDelete, "/certificates/{id}"},
			{MethodDelete, "/resources/{name}"},
			{MethodDelete, "/resources/{name}/links/{app}"},

			// Instance management
			{MethodDelete, "/instances/{id}"},
			{MethodPost, "/instances/keyroll"},

			// Registry management
			{MethodPost, "/registries"},
			{MethodDelete, "/registries/{server:.*}"},
			{MethodAny, "/v2/{path:.*}"},

			// System administration
			{MethodPut, "/system"},
			{MethodPut, "/system/jwt/rotate"},
			{MethodPost, "/system/jwt/token"},

			// Proxy service (admin only)
			{MethodAny, "/custom/http/proxy/{path:.*}"},

			// All routes (wildcard)
			{MethodAny, "/*"},
		},
	},
}

// ResolveInheritance processes the Inherits field to include parent routes
func ResolveInheritance(policies map[string]*PolicyDef) {
	resolved := make(map[string]bool)
	
	var resolve func(name string) []Route
	resolve = func(name string) []Route {
		if resolved[name] {
			return policies[name].Routes
		}
		
		policy := policies[name]
		if policy == nil {
			return nil
		}
		
		if policy.Inherits != "" {
			parentRoutes := resolve(policy.Inherits)
			// Combine parent routes with current routes (parent first)
			allRoutes := make([]Route, 0, len(parentRoutes)+len(policy.Routes))
			allRoutes = append(allRoutes, parentRoutes...)
			allRoutes = append(allRoutes, policy.Routes...)
			policy.Routes = allRoutes
			policy.Inherits = "" // Clear to avoid re-processing
		}
		
		resolved[name] = true
		return policy.Routes
	}
	
	// Resolve all policies
	for name := range policies {
		resolve(name)
	}
}