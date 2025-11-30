package rbac

import (
	"strings"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// TestAllGlobalSettingGroupsHaveHTTPRoutes ensures that every GlobalSettingGroup has
// corresponding PUT and DELETE routes defined in httpRouteSpecs with correct permissions.
// This prevents MFA policy misconfiguration errors when accessing settings pages.
func TestAllGlobalSettingGroupsHaveHTTPRoutes(t *testing.T) {
	// All GlobalSettingGroups that should have HTTP routes
	allGroups := []settings.GlobalSettingGroup{
		settings.GlobalSettingGroupMFAConfiguration,
		settings.GlobalSettingGroupAllowDestructive,
		settings.GlobalSettingGroupVCSAndCIDefaults,
		settings.GlobalSettingGroupDeployApprovals,
		settings.GlobalSettingGroupSessionConfiguration,
	}

	for _, group := range allGroups {
		expectedPath := globalSettingsGroupPath(group)
		expectedPerm := GatewayGlobalSettingGroup(group)

		t.Run(string(group)+"/PUT", func(t *testing.T) {
			perms, ok := HTTPMFAPermissions("PUT", expectedPath)
			if !ok {
				t.Errorf("Missing PUT route for GlobalSettingGroup %q (expected path: %s)", group, expectedPath)
				return
			}
			if len(perms) != 1 || perms[0] != expectedPerm {
				t.Errorf("PUT route for %q has wrong permissions: got %v, want [%s]", group, perms, expectedPerm)
			}
		})

		t.Run(string(group)+"/DELETE", func(t *testing.T) {
			perms, ok := HTTPMFAPermissions("DELETE", expectedPath)
			if !ok {
				t.Errorf("Missing DELETE route for GlobalSettingGroup %q (expected path: %s)", group, expectedPath)
				return
			}
			if len(perms) != 1 || perms[0] != expectedPerm {
				t.Errorf("DELETE route for %q has wrong permissions: got %v, want [%s]", group, perms, expectedPerm)
			}
		})
	}
}

// TestAllGlobalSettingGroupsHaveMFARequirements ensures that every GlobalSettingGroup has
// an MFA requirement defined in MFARequirements. This prevents panics when processing
// requests to settings endpoints.
func TestAllGlobalSettingGroupsHaveMFARequirements(t *testing.T) {
	allGroups := []settings.GlobalSettingGroup{
		settings.GlobalSettingGroupMFAConfiguration,
		settings.GlobalSettingGroupAllowDestructive,
		settings.GlobalSettingGroupVCSAndCIDefaults,
		settings.GlobalSettingGroupDeployApprovals,
		settings.GlobalSettingGroupSessionConfiguration,
	}

	for _, group := range allGroups {
		permission := GatewayGlobalSettingGroup(group)
		t.Run(string(group), func(t *testing.T) {
			_, ok := MFARequirements[permission]
			if !ok {
				t.Errorf("Missing MFA requirement for GlobalSettingGroup %q (permission: %s)", group, permission)
			}
		})
	}
}

// TestHTTPRoutePermissionsHaveMFARequirements validates that all HTTP routes with
// permissions have corresponding MFA requirements defined.
func TestHTTPRoutePermissionsHaveMFARequirements(t *testing.T) {
	for _, spec := range httpRouteSpecs {
		if len(spec.Permissions) == 0 {
			continue // Routes without permissions default to MFANone
		}

		for _, perm := range spec.Permissions {
			testName := spec.Method + " " + spec.Pattern + " (" + perm + ")"
			t.Run(testName, func(t *testing.T) {
				// GetMFALevel will panic if the permission is not defined and is a write operation
				// We recover and fail the test if that happens
				defer func() {
					if r := recover(); r != nil {
						if !strings.Contains(r.(string), "not in MFARequirements") {
							panic(r)
						}
						t.Errorf("Missing MFA requirement for permission %q used in route %s %s",
							perm, spec.Method, spec.Pattern)
					}
				}()

				// This will panic if the permission needs to be listed but isn't
				GetMFALevel([]string{perm})
			})
		}
	}
}
