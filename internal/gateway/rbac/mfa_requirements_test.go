package rbac

import (
	"testing"
)

func TestMFARequirements_GatewayPermissions(t *testing.T) {
	tests := []struct {
		permission string
		want       MFALevel
	}{
		// Deploy Approval - ALWAYS require fresh MFA (privileged action)
		{Gateway(ResourceDeployApprovalRequest, ActionApprove), MFAAlways},
		{Gateway(ResourceDeployApprovalRequest, ActionCreate), MFANone}, // Creating request is safe

		// API Token - ALWAYS require fresh MFA (creates long-lived credentials)
		{Gateway(ResourceAPIToken, ActionCreate), MFAAlways},
		{Gateway(ResourceAPIToken, ActionUpdate), MFAAlways},
		{Gateway(ResourceAPIToken, ActionDelete), MFAAlways},

		// User Management - ALWAYS require fresh MFA (privilege escalation)
		{Gateway(ResourceUser, ActionCreate), MFAAlways},
		{Gateway(ResourceUser, ActionUpdate), MFAAlways}, // Role changes
		{Gateway(ResourceUser, ActionDelete), MFAAlways},

		// MFA Methods - ALWAYS require fresh MFA (disabling security)
		{Auth(ResourceMFAMethod, ActionDelete), MFAAlways},
		{Auth(ResourceMFAMethod, ActionCreate), MFANone},   // Enrolling is safe
		{Auth(ResourceMFAMethod, ActionUpdate), MFAStepUp}, // Updating settings

		// Security Settings - ALWAYS require fresh MFA
		{Security(ResourceSecret, ActionUpdate), MFAAlways},

		// Integrations - Step-up for configuration
		{Gateway(ResourceIntegration, ActionCreate), MFAStepUp},
		{Gateway(ResourceIntegration, ActionUpdate), MFAStepUp},
		{Gateway(ResourceIntegration, ActionDelete), MFAStepUp},
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
		{Convox(ResourceApp, ActionDelete), MFAAlways},
		{Convox(ResourceInstance, ActionTerminate), MFAAlways},

		// Sensitive operations - Step-up
		{Convox(ResourceApp, ActionCreate), MFAStepUp},
		{Convox(ResourceApp, ActionUpdate), MFAStepUp},
		{Convox(ResourceApp, ActionRestart), MFAStepUp},
		{Convox(ResourceRelease, ActionPromote), MFAStepUp},
		{Convox(ResourceRelease, ActionCreate), MFAStepUp},
		{Convox(ResourceBuild, ActionCreate), MFAStepUp},
		{Convox(ResourceProcess, ActionExec), MFAStepUp},
		{Convox(ResourceProcess, ActionStart), MFAStepUp},
		{Convox(ResourceProcess, ActionStop), MFAStepUp},
		{Convox(ResourceProcess, ActionTerminate), MFAStepUp},
		{Convox(ResourceEnv, ActionSet), MFAStepUp},
		{Convox(ResourceRack, ActionUpdate), MFAStepUp},

		// Low-risk write operations - None
		{Convox(ResourceObject, ActionCreate), MFANone}, // Object uploads for builds
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
				Convox(ResourceApp, ActionList),         // None
				Convox(ResourceRelease, ActionPromote),  // Step-up
				Gateway(ResourceAPIToken, ActionCreate), // Always
			},
			want: MFAAlways,
		},
		{
			name: "highest is MFAStepUp",
			permissions: []string{
				Convox(ResourceApp, ActionList),        // None
				Convox(ResourceRelease, ActionPromote), // Step-up
			},
			want: MFAStepUp,
		},
		{
			name: "all MFANone",
			permissions: []string{
				Convox(ResourceApp, ActionList), // None
				Convox(ResourceApp, ActionRead), // None
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
			permissions: []string{Gateway(ResourceDeployApprovalRequest, ActionApprove)},
			want:        true,
		},
		{
			name:        "API token create requires MFAAlways",
			permissions: []string{Gateway(ResourceAPIToken, ActionCreate)},
			want:        true,
		},
		{
			name:        "release promote only requires MFAStepUp",
			permissions: []string{Convox(ResourceRelease, ActionPromote)},
			want:        false,
		},
		{
			name:        "read operations don't require MFAAlways",
			permissions: []string{Convox(ResourceApp, ActionRead)},
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
			permissions: []string{Gateway(ResourceDeployApprovalRequest, ActionApprove)},
			want:        true, // MFAAlways >= MFAStepUp
		},
		{
			name:        "release promote requires step-up",
			permissions: []string{Convox(ResourceRelease, ActionPromote)},
			want:        true,
		},
		{
			name:        "read operations don't require step-up",
			permissions: []string{Convox(ResourceApp, ActionRead)},
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
			permission: Convox(ResourceEnv, ActionRead),
			want:       MFANone,
		},
		{
			name:       "convox:env:list defaults to MFANone",
			permission: Convox(ResourceEnv, ActionList),
			want:       MFANone,
		},
		{
			name:       "convox:deploy:read defaults to MFANone",
			permission: Convox(ResourceDeploy, ActionRead),
			want:       MFANone,
		},
		{
			name:       "convox:deploy:list defaults to MFANone",
			permission: Convox(ResourceDeploy, ActionList),
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
			permission: Convox(ResourceDeploy, ActionCreate),
		},
		{
			name:       "unlisted env:update should panic",
			permission: Convox(ResourceEnv, ActionUpdate),
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
