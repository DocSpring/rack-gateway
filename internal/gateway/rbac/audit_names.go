package rbac

import (
	"net/url"
	"strings"
)

// revealRoutes are the gateway reads that return secret and protected values when their query asks for them (a
// reveal parameter set to "true"). Those requests are named after what they reveal.
var revealRoutes = map[string]revealRoute{
	// The web env editor returns secret and protected values with ?secrets=true.
	"GET /api/v1/apps/:app/env": {param: "secrets", action: "secret.read"},
}

type revealRoute struct {
	// param is the query parameter that asks for secret values (="true").
	param string
	// action names the audit entries of requests that ask for them.
	action string
}

// auditActions names the audit entries of gateway routes that share their permission with other routes, so the
// permission doesn't say what they do. A route without a permission names them itself (RouteSpec.AuditAction). Every
// other route is named after its permission: a route needing gateway:user:create is user.create.
var auditActions = map[string]string{
	"POST /api/v1/users/:email/lock":                       "user.lock",
	"POST /api/v1/users/:email/unlock":                     "user.unlock",
	"POST /api/v1/users/:email/mfa/reset":                  "user.mfa_reset",
	"POST /api/v1/users/:email/sessions/:sessionID/revoke": "user.sessions.revoke",
	"POST /api/v1/users/:email/sessions/revoke_all":        "user.sessions.revoke_all",
	"POST /api/v1/deploy-approval-requests/:id/reject":     "deploy_approval_request.reject",
	"POST /api/v1/deploy-approval-requests/:id/extend":     "deploy_approval_request.extend",
	"POST /api/v1/integrations/slack/test":                 "integration.test",
	"POST /api/v1/diagnostics/sentry":                      "integration.sentry_test",
	"POST /api/v1/settings/rack-tls-cert/refresh":          "rack_tls_cert.refresh",
	"POST /api/v1/jobs/:id/retry":                          "job.retry",
	// MFA verification shares auth:mfa_verification:create (audit.ActionMFAVerify and ActionMFAChallenge)
	"POST /api/v1/auth/mfa/verify":                    "mfa.verify",
	"POST /api/v1/auth/mfa/webauthn/assertion/verify": "mfa.verify",
	"POST /api/v1/auth/mfa/webauthn/assertion/start":  "mfa.challenge",
}

// AuditName is how a route's audit entries name its requests.
type AuditName struct {
	// ActionType is the scope of the route's permission: gateway, convox, auth or security.
	ActionType string
	// Action is <resource>.<verb>, e.g. user.create.
	Action string
	// ResourceType is the action's resource, e.g. user.
	ResourceType string
}

// AuditName names the audit entries of a request to the route with this query string.
func (r *RouteSpec) AuditName(query url.Values) AuditName {
	key := httpRouteKey(r.Method, r.Pattern)
	scope, resource, verb := r.permissionParts()
	action := resource + "." + verb
	switch {
	case r.revealRequested(query):
		action = revealRoutes[key].action
	case r.AuditAction != "":
		action = r.AuditAction
	case auditActions[key] != "":
		action = auditActions[key]
	case resource == "setting" || resource == "setting_group":
		action = "setting." + methodVerb(r.Method)
	}
	if scope == "" {
		scope = ScopeGateway.String()
	}
	resourceType, _, _ := strings.Cut(action, ".")
	return AuditName{ActionType: scope, Action: action, ResourceType: resourceType}
}

// revealRequested reports whether a request to the route with this query asks for secret values.
func (r *RouteSpec) revealRequested(query url.Values) bool {
	reveal, ok := revealRoutes[httpRouteKey(r.Method, r.Pattern)]
	return ok && strings.EqualFold(query.Get(reveal.param), "true")
}

// permissionParts splits the route's first permission into scope, resource and verb (all empty without one).
func (r *RouteSpec) permissionParts() (scope, resource, verb string) {
	if len(r.Permissions) == 0 {
		return "", "", ""
	}
	parts := strings.SplitN(r.Permissions[0], ":", 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// methodVerb names what a request with this method does to a setting.
func methodVerb(method string) string {
	switch method {
	case "GET", "HEAD":
		return "read"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return strings.ToLower(method)
	}
}
