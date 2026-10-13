package rbac_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// Every write permission that a rack route, gateway route or CLI command needs has its own entry in
// MFARequirements. An unlisted one fails closed to MFAAlways, which is safe, but nobody chose that level: the CLI
// then can't tell its user in advance, and the docs don't list it.
func TestEveryWritePermissionHasAnMFALevel(t *testing.T) {
	usedBy := map[string][]string{}
	for _, spec := range append(rbac.RackRouteSpecs(), rbac.HTTPRouteSpecs()...) {
		for _, perm := range spec.PermissionStrings() {
			usedBy[perm] = append(usedBy[perm], spec.Method+" "+spec.Pattern)
		}
	}
	for _, cmd := range convox.Commands {
		for _, perm := range cmd.Permissions {
			usedBy[perm] = append(usedBy[perm], "convox "+cmd.Command)
		}
	}

	var missing []string
	for perm, users := range usedBy {
		if strings.HasSuffix(perm, ":read") || strings.HasSuffix(perm, ":list") {
			continue
		}
		if _, ok := rbac.MFARequirements[perm]; !ok {
			missing = append(missing, perm+" (used by "+strings.Join(users, ", ")+")")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("no MFA level in MFARequirements for %s", m)
	}
}

// Rack operations that delete an app or skip the checks a build gets need MFA on the request. The other rack writes
// need it within the step-up window.
func TestRackWriteMFALevels(t *testing.T) {
	cases := map[string]rbac.MFALevel{
		rbac.Convox(rbac.ResourceBuild, rbac.ActionImport):  rbac.MFAAlways,
		rbac.Convox(rbac.ResourceBuild, rbac.ActionUpdate):  rbac.MFAStepUp,
		rbac.Convox(rbac.ResourceBuild, rbac.ActionCreate):  rbac.MFAStepUp,
		rbac.Convox(rbac.ResourceObject, rbac.ActionCreate): rbac.MFANone,
		rbac.Convox(rbac.ResourceApp, rbac.ActionDelete):    rbac.MFAAlways,
	}
	for perm, want := range cases {
		if got := rbac.MFALevelForPermission(perm); got != want {
			t.Errorf("MFALevelForPermission(%q) = %v, want %v", perm, got, want)
		}
	}
}

// Changing which env vars are protected or secret needs MFA on that request, as changing a protected var does.
// Within a step-up window, a var could otherwise be unprotected, changed and protected again.
func TestEnvVarListSettingsNeedMFAOnTheRequest(t *testing.T) {
	keys := []settings.AppSettingKey{settings.AppSettingProtectedEnvVars, settings.AppSettingSecretEnvVars}
	for _, key := range keys {
		if got := rbac.MFALevelForPermission(rbac.GatewayAppSetting(key)); got != rbac.MFAAlways {
			t.Errorf("MFA level for the %s setting = %v, want %v", key, got, rbac.MFAAlways)
		}
	}
}
