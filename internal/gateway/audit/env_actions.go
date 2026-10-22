package audit

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// EnvChangeAction returns the audit action and resource type for an env change. Protected keys get their own
// actions, so every protected change stands out in the audit log and can be routed to Slack. Only a removed key
// is an unset: a key set to an empty value is a set.
func EnvChangeAction(d envutil.EnvDiff) (string, string) {
	switch {
	case d.Protected && d.Removed:
		return BuildAction(rbac.ResourceEnv.String(), ActionVerbUnsetProtected), "env"
	case d.Protected:
		return BuildAction(rbac.ResourceEnv.String(), rbac.ActionSetProtected.String()), "env"
	case d.Secret && d.Removed:
		return BuildAction(rbac.ResourceSecret.String(), ActionVerbUnset), "secret"
	case d.Secret:
		return BuildAction(rbac.ResourceSecret.String(), rbac.ActionSet.String()), "secret"
	case d.Removed:
		return BuildAction(rbac.ResourceEnv.String(), ActionVerbUnset), "env"
	default:
		return BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()), "env"
	}
}
