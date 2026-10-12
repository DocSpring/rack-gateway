package rbac

import (
	"net/url"
	"testing"
)

// TestAuditTablesOnlyNameRegisteredRoutes catches a typo or a stale entry in the naming tables, which would
// otherwise silently leave a route on its default name.
func TestAuditTablesOnlyNameRegisteredRoutes(t *testing.T) {
	keys := make([]string, 0, len(revealRoutes)+len(auditActions))
	for key := range revealRoutes {
		keys = append(keys, key)
	}
	for key := range auditActions {
		keys = append(keys, key)
	}
	for _, key := range keys {
		if _, ok := httpRouteIndex[key]; !ok {
			t.Errorf("%s is in the audit tables but isn't a gateway route", key)
		}
	}
}

func TestRevealRouteIsNamedAfterWhatItReveals(t *testing.T) {
	spec, ok := LookupHTTPRoute("GET", "/api/v1/apps/:app/env")
	if !ok {
		t.Fatal("missing env route")
	}
	cases := map[string]struct {
		query  string
		action string
	}{
		"plain read":      {"", "env.read"},
		"one key":         {"key=PORT", "env.read"},
		"secrets=false":   {"secrets=false", "env.read"},
		"secrets=true":    {"secrets=true", "secret.read"},
		"secrets=TRUE":    {"secrets=TRUE&key=API_KEY", "secret.read"},
		"repeated params": {"secrets=true&secrets=false", "secret.read"},
	}
	for name, tc := range cases {
		query, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		if got := spec.AuditName(query).Action; got != tc.action {
			t.Errorf("%s: action = %q, want %q", name, got, tc.action)
		}
	}
}

func TestAuditNames(t *testing.T) {
	cases := []struct {
		method, pattern string
		want            AuditName
	}{
		{"POST", "/api/v1/users", AuditName{"gateway", "user.create", "user"}},
		{"POST", "/api/v1/users/:email/lock", AuditName{"gateway", "user.lock", "user"}},
		{"POST", "/api/v1/deploy-approval-requests/:id/reject", AuditName{
			"gateway", "deploy_approval_request.reject", "deploy_approval_request",
		}},
		{"PUT", "/api/v1/settings/mfa-configuration", AuditName{"gateway", "setting.update", "setting"}},
		{"DELETE", "/api/v1/apps/:app/settings/protected-env-vars", AuditName{"gateway", "setting.delete", "setting"}},
		{"DELETE", "/api/v1/auth/mfa/methods/:methodID", AuditName{"auth", "mfa_method.delete", "mfa_method"}},
		{"POST", "/api/v1/auth/mfa/verify", AuditName{"auth", "mfa.verify", "mfa"}},
		{"POST", "/api/v1/auth/mfa/webauthn/assertion/verify", AuditName{"auth", "mfa.verify", "mfa"}},
		{"POST", "/api/v1/auth/mfa/webauthn/assertion/start", AuditName{"auth", "mfa.challenge", "mfa"}},
		{"PUT", "/api/v1/apps/:app/env", AuditName{"convox", "env.set", "env"}},
		{"GET", "/api/v1/info", AuditName{"gateway", "http.get", "http"}},
		{"GET", "/api/v1/users", AuditName{"gateway", "user.list", "user"}},
	}
	for _, tc := range cases {
		spec, ok := LookupHTTPRoute(tc.method, tc.pattern)
		if !ok {
			t.Fatalf("missing route %s %s", tc.method, tc.pattern)
		}
		if got := spec.AuditName(nil); got != tc.want {
			t.Errorf("%s %s: AuditName = %+v, want %+v", tc.method, tc.pattern, got, tc.want)
		}
	}
}
