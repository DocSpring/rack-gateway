package rbac

import (
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// Authorization and MFA policy for every authenticated gateway (non-proxy) route.
// The Authorize middleware denies any route missing from this table.

func routeSlug(segment string) string {
	return strings.ReplaceAll(segment, "_", "-")
}

func appSettingPath(key settings.AppSettingKey) string {
	return "/api/v1/apps/:app/settings/" + routeSlug(key.String())
}

func appSettingsGroupPath(group settings.AppSettingGroup) string {
	return "/api/v1/apps/:app/settings/" + routeSlug(string(group))
}

func globalSettingsGroupPath(group settings.GlobalSettingGroup) string {
	return "/api/v1/settings/" + routeSlug(string(group))
}

func settingsActionPath(segment string) string {
	return "/api/v1/settings/" + routeSlug(segment)
}

var httpRouteSpecs = []RouteSpec{
	// MFA management
	newSelfRoute("GET", "/api/v1/auth/mfa/status"),
	newSelfRoute("POST", "/api/v1/auth/mfa/enroll/totp/start", Auth(ResourceMFAMethod, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/enroll/totp/confirm", Auth(ResourceMFAMethod, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/enroll/yubiotp/start", Auth(ResourceMFAMethod, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/enroll/webauthn/start", Auth(ResourceMFAMethod, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/enroll/webauthn/confirm", Auth(ResourceMFAMethod, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/verify", Auth(ResourceMFAVerification, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/webauthn/assertion/start", Auth(ResourceMFAVerification, ActionCreate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/webauthn/assertion/verify", Auth(ResourceMFAVerification, ActionCreate)),
	newSelfRoute("PUT", "/api/v1/auth/mfa/preferred-method", Auth(ResourceMFAPreferences, ActionUpdate)),
	newSelfRoute("PUT", "/api/v1/auth/mfa/methods/:methodID", Auth(ResourceMFAMethod, ActionUpdate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/backup-codes/regenerate", Auth(ResourceMFABackupCodes, ActionGenerate)),
	newSelfRoute("POST", "/api/v1/auth/mfa/trusted-devices/trust", Auth(ResourceTrustedDevice, ActionCreate)),
	newSelfRoute("DELETE", "/api/v1/auth/mfa/trusted-devices/:deviceID", Auth(ResourceTrustedDevice, ActionDelete)),
	newSelfRoute("DELETE", "/api/v1/auth/mfa/methods/:methodID", Auth(ResourceMFAMethod, ActionDelete)),

	// Authenticated info
	{Method: "GET", Pattern: "/api/v1/info", Access: AccessAuthenticated, AllowAPIToken: true},
	newSelfRoute("GET", "/api/v1/created-by"),
	newTokenRoute("GET", "/api/v1/rack", Convox(ResourceRack, ActionRead)),
	// Listing every request (and a request's audit trail) is for approvers. The handlers also check
	// approver permission; the route uses the read-level :list permission so these reads don't
	// inherit :approve's MFAAlways requirement.
	newHTTPRoute("GET", "/api/v1/deploy-approval-requests", Gateway(ResourceDeployApprovalRequest, ActionList)),
	newTokenRoute("GET", "/api/v1/deploy-approval-requests/:id", Gateway(ResourceDeployApprovalRequest, ActionRead)),
	newHTTPRoute(
		"GET",
		"/api/v1/deploy-approval-requests/:id/audit-logs",
		Gateway(ResourceDeployApprovalRequest, ActionList),
	),
	newTokenRoute("POST", "/api/v1/deploy-approval-requests", Gateway(ResourceDeployApprovalRequest, ActionCreate)),
	newHTTPRoute(
		"POST",
		"/api/v1/deploy-approval-requests/:id/approve",
		Gateway(ResourceDeployApprovalRequest, ActionApprove),
	),
	newHTTPRoute(
		"POST",
		"/api/v1/deploy-approval-requests/:id/reject",
		Gateway(ResourceDeployApprovalRequest, ActionApprove),
	),
	newHTTPRoute(
		"POST",
		"/api/v1/deploy-approval-requests/:id/extend",
		Gateway(ResourceDeployApprovalRequest, ActionApprove),
	),
	newHTTPRoute("GET", "/api/v1/apps/:app/env", Convox(ResourceEnv, ActionRead)),
	newHTTPRoute("PUT", "/api/v1/apps/:app/env", Convox(ResourceEnv, ActionSet)),

	// Web-safe Convox proxies
	newHTTPRoute("GET", "/api/v1/convox/apps", Convox(ResourceApp, ActionList)),
	newHTTPRoute("GET", "/api/v1/convox/apps/*path", Convox(ResourceApp, ActionRead)),
	newHTTPRoute("PUT", "/api/v1/convox/apps/:app/services/:name", Convox(ResourceApp, ActionUpdate)),
	newHTTPRoute(
		"DELETE",
		"/api/v1/convox/apps/:app/processes/:pid",
		Convox(ResourceProcess, ActionTerminate),
	),
	newHTTPRoute("GET", "/api/v1/convox/instances", Convox(ResourceInstance, ActionList)),
	newHTTPRoute("GET", "/api/v1/convox/system/processes", Convox(ResourceRack, ActionRead)),

	// Configuration & diagnostics
	newSelfRoute("GET", "/api/v1/settings"),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupMFAConfiguration),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupMFAConfiguration),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupMFAConfiguration),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupMFAConfiguration),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupAllowDestructive),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupAllowDestructive),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupAllowDestructive),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupAllowDestructive),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupVCSAndCIDefaults),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupVCSAndCIDefaults),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupVCSAndCIDefaults),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupVCSAndCIDefaults),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupDeployApprovals),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupDeployApprovals),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupDeployApprovals),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupDeployApprovals),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupSessionConfiguration),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupSessionConfiguration),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupSessionConfiguration),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupSessionConfiguration),
	),
	newHTTPRoute("POST", settingsActionPath("rack_tls_cert/refresh"), Security(ResourceSecret, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/diagnostics/sentry", Gateway(ResourceIntegration, ActionUpdate)),

	// Users & roles
	newSelfRoute("GET", "/api/v1/roles"),
	newHTTPRoute("GET", "/api/v1/users", Gateway(ResourceUser, ActionRead)),
	newHTTPRoute("GET", "/api/v1/users/:email", Gateway(ResourceUser, ActionRead)),
	newHTTPRoute("POST", "/api/v1/users", Gateway(ResourceUser, ActionCreate)),
	newHTTPRoute("DELETE", "/api/v1/users/:email", Gateway(ResourceUser, ActionDelete)),
	newHTTPRoute("PUT", "/api/v1/users/:email", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("PUT", "/api/v1/users/:email/name", Gateway(ResourceUser, ActionUpdateName)),
	newHTTPRoute("GET", "/api/v1/users/:email/sessions", Gateway(ResourceUser, ActionRead)),
	newHTTPRoute("POST", "/api/v1/users/:email/sessions/:sessionID/revoke", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/users/:email/sessions/revoke_all", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/users/:email/lock", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/users/:email/unlock", Gateway(ResourceUser, ActionUpdate)),

	// Audit logs
	newHTTPRoute("GET", "/api/v1/audit-logs", Gateway(ResourceAuditLog, ActionRead)),
	newHTTPRoute("GET", "/api/v1/audit-logs/export", Gateway(ResourceAuditLog, ActionRead)),

	// API tokens
	newHTTPRoute("GET", "/api/v1/api-tokens", Gateway(ResourceAPIToken, ActionRead)),
	newSelfRoute("GET", "/api/v1/api-tokens/permissions"),
	newHTTPRoute("GET", "/api/v1/api-tokens/:tokenID", Gateway(ResourceAPIToken, ActionRead)),
	newHTTPRoute("POST", "/api/v1/api-tokens", Gateway(ResourceAPIToken, ActionCreate)),
	newHTTPRoute("PUT", "/api/v1/api-tokens/:tokenID", Gateway(ResourceAPIToken, ActionUpdate)),
	newHTTPRoute("DELETE", "/api/v1/api-tokens/:tokenID", Gateway(ResourceAPIToken, ActionDelete)),

	// Background jobs
	newHTTPRoute("GET", "/api/v1/jobs", Gateway(ResourceJob, ActionList)),
	newHTTPRoute("GET", "/api/v1/jobs/:id", Gateway(ResourceJob, ActionRead)),
	newHTTPRoute("DELETE", "/api/v1/jobs/:id", Gateway(ResourceJob, ActionDelete)),
	newHTTPRoute("POST", "/api/v1/jobs/:id/retry", Gateway(ResourceJob, ActionUpdate)),

	// Integrations
	newHTTPRoute("GET", "/api/v1/integrations/slack", Gateway(ResourceIntegration, ActionRead)),
	newHTTPRoute("POST", "/api/v1/integrations/slack/oauth/authorize", Gateway(ResourceIntegration, ActionCreate)),
	newHTTPRoute("GET", "/api/v1/integrations/slack/oauth/callback", Gateway(ResourceIntegration, ActionCreate)),
	newHTTPRoute("PUT", "/api/v1/integrations/slack/channels", Gateway(ResourceIntegration, ActionUpdate)),
	newHTTPRoute("PUT", "/api/v1/integrations/slack/alerts", Gateway(ResourceIntegration, ActionUpdate)),
	newHTTPRoute("DELETE", "/api/v1/integrations/slack", Gateway(ResourceIntegration, ActionDelete)),
	newHTTPRoute("GET", "/api/v1/integrations/slack/channels/list", Gateway(ResourceIntegration, ActionRead)),
	newHTTPRoute("POST", "/api/v1/integrations/slack/test", Gateway(ResourceIntegration, ActionUpdate)),

	// App-specific settings
	newSelfRoute("GET", "/api/v1/apps/:app/settings"),
	newHTTPRoute(
		"PUT",
		appSettingsGroupPath(settings.AppSettingGroupVCSCIDeploy),
		GatewayAppSettingGroup(settings.AppSettingGroupVCSCIDeploy),
	),
	newHTTPRoute(
		"DELETE",
		appSettingsGroupPath(settings.AppSettingGroupVCSCIDeploy),
		GatewayAppSettingGroup(settings.AppSettingGroupVCSCIDeploy),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingProtectedEnvVars),
		GatewayAppSetting(settings.AppSettingProtectedEnvVars),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingProtectedEnvVars),
		GatewayAppSetting(settings.AppSettingProtectedEnvVars),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingSecretEnvVars),
		GatewayAppSetting(settings.AppSettingSecretEnvVars),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingSecretEnvVars),
		GatewayAppSetting(settings.AppSettingSecretEnvVars),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingApprovedDeployCommands),
		GatewayAppSetting(settings.AppSettingApprovedDeployCommands),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingApprovedDeployCommands),
		GatewayAppSetting(settings.AppSettingApprovedDeployCommands),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingServiceImagePatterns),
		GatewayAppSetting(settings.AppSettingServiceImagePatterns),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingServiceImagePatterns),
		GatewayAppSetting(settings.AppSettingServiceImagePatterns),
	),
}
