package convox

import "fmt"

// MFALevel defines the MFA verification requirement level
type MFALevel int

const (
	// MFANone - No MFA required beyond enrollment
	MFANone MFALevel = iota
	// MFAStepUp - MFA required within time window (default 5 minutes)
	MFAStepUp
	// MFAAlways - MFA required immediately before action, no time window
	MFAAlways
)

// MFARequirements is the EXHAUSTIVE list of ALL permissions and their MFA level
// This is shared between the gateway (for API enforcement) and the CLI (for pre-emptive step-up)
// CRITICAL: This list must be complete. If a permission is missing, GetMFALevel will panic.
var MFARequirements = map[string]MFALevel{
	// DESTRUCTIVE OPERATIONS - Always require fresh MFA, no time window
	"convox:app:delete":         MFAAlways,
	"convox:resource:delete":    MFAAlways,
	"convox:instance:terminate": MFAAlways,
	"convox:cert:delete":        MFAAlways,
	"convox:registry:remove":    MFAAlways,

	// SENSITIVE OPERATIONS - Step-up within time window
	// App lifecycle
	"convox:app:create":  MFAStepUp,
	"convox:app:update":  MFAStepUp,
	"convox:app:restart": MFAStepUp,

	// Deployments and builds
	"convox:build:create":    MFAStepUp,
	"convox:release:create":  MFAStepUp,
	"convox:release:promote": MFAStepUp,

	// Process control
	"convox:process:exec":      MFAStepUp,
	"convox:process:terminate": MFAStepUp,
	"convox:process:start":     MFAStepUp,

	// Environment variables (secrets)
	"convox:env:set":   MFAStepUp,
	"convox:env:unset": MFAStepUp,

	// Rack operations
	"convox:rack:update": MFAStepUp,

	// Certificates
	"convox:cert:generate": MFAStepUp,
	"convox:cert:import":   MFAStepUp,
	"convox:cert:update":   MFAStepUp,

	// Registries
	"convox:registry:add": MFAStepUp,

	// Resources
	"convox:resource:create": MFAStepUp,
	"convox:resource:update": MFAStepUp,

	// Instances
	"convox:instance:keyroll": MFAStepUp,

	// READ-ONLY OPERATIONS - No MFA required beyond enrollment
	"convox:app:list":      MFANone,
	"convox:app:read":      MFANone,
	"convox:build:list":    MFANone,
	"convox:build:read":    MFANone,
	"convox:release:list":  MFANone,
	"convox:release:read":  MFANone,
	"convox:process:list":  MFANone,
	"convox:process:read":  MFANone,
	"convox:env:read":      MFANone,
	"convox:log:read":      MFANone,
	"convox:rack:read":     MFANone,
	"convox:resource:list": MFANone,
	"convox:resource:read": MFANone,
	"convox:instance:list": MFANone,
	"convox:instance:read": MFANone,
	"convox:cert:list":     MFANone,
	"convox:registry:list": MFANone,
	"convox:object:create": MFANone, // Object uploads for builds
}

// GetMFALevel returns the highest MFA level required by any of the given permissions
// Panics if any permission is not found in MFARequirements - this is intentional to catch missing definitions
func GetMFALevel(permissions []string) MFALevel {
	highest := MFANone
	for _, perm := range permissions {
		level, ok := MFARequirements[perm]
		if !ok {
			panic(fmt.Sprintf("CRITICAL: Permission %q not found in MFARequirements - this list must be exhaustive", perm))
		}
		if level > highest {
			highest = level
		}
	}
	return highest
}

// RequiresMFAStepUp returns true if any of the given permissions require MFA step-up (time window)
func RequiresMFAStepUp(permissions []string) bool {
	return GetMFALevel(permissions) >= MFAStepUp
}

// RequiresMFAAlways returns true if any of the given permissions require immediate MFA
func RequiresMFAAlways(permissions []string) bool {
	return GetMFALevel(permissions) == MFAAlways
}
