package convox

import (
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func TestMFARequirements_GatewayPermissions(t *testing.T) {
	tests := []struct {
		permission string
		want       MFALevel
	}{
		// Deploy Approval - ALWAYS require fresh MFA (privileged action)
		{rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionApprove), MFAAlways},
		{rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionCreate), MFANone}, // Creating request is safe

		// API Token - ALWAYS require fresh MFA (creates long-lived credentials)
		{rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionCreate), MFAAlways},
		{rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionUpdate), MFAAlways},
		{rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionDelete), MFAAlways},

		// User Management - ALWAYS require fresh MFA (privilege escalation)
		{rbac.Gateway(rbac.ResourceUser, rbac.ActionCreate), MFAAlways},
		{rbac.Gateway(rbac.ResourceUser, rbac.ActionUpdate), MFAAlways}, // Role changes
		{rbac.Gateway(rbac.ResourceUser, rbac.ActionDelete), MFAAlways},

		// MFA Methods - ALWAYS require fresh MFA (disabling security)
		{rbac.Auth(rbac.ResourceMFAMethod, rbac.ActionDelete), MFAAlways},
		{rbac.Auth(rbac.ResourceMFAMethod, rbac.ActionCreate), MFANone},   // Enrolling is safe
		{rbac.Auth(rbac.ResourceMFAMethod, rbac.ActionUpdate), MFAStepUp}, // Updating settings

		// Security Settings - ALWAYS require fresh MFA
		{rbac.Security(rbac.ResourceSecret, rbac.ActionUpdate), MFAAlways},

		// Integrations - Step-up for configuration
		{rbac.Gateway(rbac.ResourceIntegration, rbac.ActionCreate), MFAStepUp},
		{rbac.Gateway(rbac.ResourceIntegration, rbac.ActionUpdate), MFAStepUp},
		{rbac.Gateway(rbac.ResourceIntegration, rbac.ActionDelete), MFAStepUp},
	}

	for _, tt := range tests {
		t.Run(tt.permission, func(t *testing.T) {
			got := GetMFALevel([]string{tt.permission})
			if got != tt.want {
				t.Errorf("GetMFALevel(%q) = %v, want %v", tt.permission, got, tt.want)
			}
		})
	}
}

func TestMFARequirements_ConvoxPermissions(t *testing.T) {
	tests := []struct {
		permission string
		want       MFALevel
	}{
		// Destructive operations - ALWAYS
		{rbac.Convox(rbac.ResourceApp, rbac.ActionDelete), MFAAlways},
		{rbac.Convox(rbac.ResourceInstance, rbac.ActionTerminate), MFAAlways},

		// Sensitive operations - Step-up
		{rbac.Convox(rbac.ResourceApp, rbac.ActionCreate), MFAStepUp},
		{rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate), MFAStepUp},
		{rbac.Convox(rbac.ResourceApp, rbac.ActionRestart), MFAStepUp},
		{rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote), MFAStepUp},
		{rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate), MFAStepUp},
		{rbac.Convox(rbac.ResourceBuild, rbac.ActionCreate), MFAStepUp},
		{rbac.Convox(rbac.ResourceProcess, rbac.ActionExec), MFAStepUp},
		{rbac.Convox(rbac.ResourceProcess, rbac.ActionStart), MFAStepUp},
		{rbac.Convox(rbac.ResourceProcess, rbac.ActionStop), MFAStepUp},
		{rbac.Convox(rbac.ResourceProcess, rbac.ActionTerminate), MFAStepUp},
		{rbac.Convox(rbac.ResourceEnv, rbac.ActionSet), MFAStepUp},
		{rbac.Convox(rbac.ResourceRack, rbac.ActionUpdate), MFAStepUp},

		// Low-risk write operations - None
		{rbac.Convox(rbac.ResourceObject, rbac.ActionCreate), MFANone}, // Object uploads for builds
	}

	for _, tt := range tests {
		t.Run(tt.permission, func(t *testing.T) {
			got := GetMFALevel([]string{tt.permission})
			if got != tt.want {
				t.Errorf("GetMFALevel(%q) = %v, want %v", tt.permission, got, tt.want)
			}
		})
	}
}

func TestGetMFALevel_MultiplePermissions(t *testing.T) {
	tests := []struct {
		name        string
		permissions []string
		want        MFALevel
	}{
		{
			name: "highest is MFAAlways",
			permissions: []string{
				rbac.Convox(rbac.ResourceApp, rbac.ActionList),         // None
				rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote),  // Step-up
				rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionCreate), // Always
			},
			want: MFAAlways,
		},
		{
			name: "highest is MFAStepUp",
			permissions: []string{
				rbac.Convox(rbac.ResourceApp, rbac.ActionList),        // None
				rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote), // Step-up
			},
			want: MFAStepUp,
		},
		{
			name: "all MFANone",
			permissions: []string{
				rbac.Convox(rbac.ResourceApp, rbac.ActionList), // None
				rbac.Convox(rbac.ResourceApp, rbac.ActionRead), // None
			},
			want: MFANone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMFALevel(tt.permissions)
			if got != tt.want {
				t.Errorf("GetMFALevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequiresMFAAlways(t *testing.T) {
	tests := []struct {
		name        string
		permissions []string
		want        bool
	}{
		{
			name:        "deploy approval approve requires MFAAlways",
			permissions: []string{rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)},
			want:        true,
		},
		{
			name:        "API token create requires MFAAlways",
			permissions: []string{rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionCreate)},
			want:        true,
		},
		{
			name:        "release promote only requires MFAStepUp",
			permissions: []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote)},
			want:        false,
		},
		{
			name:        "read operations don't require MFAAlways",
			permissions: []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRead)},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequiresMFAAlways(tt.permissions)
			if got != tt.want {
				t.Errorf("RequiresMFAAlways() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequiresMFAStepUp(t *testing.T) {
	tests := []struct {
		name        string
		permissions []string
		want        bool
	}{
		{
			name:        "deploy approval approve requires step-up (actually MFAAlways)",
			permissions: []string{rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)},
			want:        true, // MFAAlways >= MFAStepUp
		},
		{
			name:        "release promote requires step-up",
			permissions: []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote)},
			want:        true,
		},
		{
			name:        "read operations don't require step-up",
			permissions: []string{rbac.Convox(rbac.ResourceApp, rbac.ActionRead)},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequiresMFAStepUp(tt.permissions)
			if got != tt.want {
				t.Errorf("RequiresMFAStepUp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMFALevel_AllowsReadListWithoutExplicitListing(t *testing.T) {
	// These permissions are NOT explicitly listed in MFARequirements
	// but should default to MFANone since they're read-only
	tests := []struct {
		name       string
		permission string
		want       MFALevel
	}{
		{
			name:       "convox:env:read defaults to MFANone",
			permission: rbac.Convox(rbac.ResourceEnv, rbac.ActionRead),
			want:       MFANone,
		},
		{
			name:       "convox:env:list defaults to MFANone",
			permission: rbac.Convox(rbac.ResourceEnv, rbac.ActionList),
			want:       MFANone,
		},
		{
			name:       "convox:deploy:read defaults to MFANone",
			permission: rbac.Convox(rbac.ResourceDeploy, rbac.ActionRead),
			want:       MFANone,
		},
		{
			name:       "convox:deploy:list defaults to MFANone",
			permission: rbac.Convox(rbac.ResourceDeploy, rbac.ActionList),
			want:       MFANone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMFALevel([]string{tt.permission})
			if got != tt.want {
				t.Errorf("GetMFALevel(%q) = %v, want %v", tt.permission, got, tt.want)
			}
		})
	}
}

func TestGetMFALevel_PanicsForUnlistedWriteOperations(t *testing.T) {
	// These permissions are NOT explicitly listed and are NOT read/list
	// so GetMFALevel should panic
	tests := []struct {
		name       string
		permission string
	}{
		{
			name:       "unlisted deploy:create should panic",
			permission: rbac.Convox(rbac.ResourceDeploy, rbac.ActionCreate),
		},
		{
			name:       "unlisted env:update should panic",
			permission: rbac.Convox(rbac.ResourceEnv, rbac.ActionUpdate),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("GetMFALevel(%q) did not panic, but should have", tt.permission)
				}
			}()
			GetMFALevel([]string{tt.permission})
		})
	}
}
