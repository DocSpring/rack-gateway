package audit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// rbac names the MFA verification routes with these actions, as strings (it can't import them), and the CLI login
// records its MFA step with ActionMFAVerify: one name for every MFA verification.
func TestMFAVerificationRoutesUseTheMFAActions(t *testing.T) {
	cases := []struct {
		method, pattern, action string
	}{
		{"POST", "/api/v1/auth/mfa/verify", ActionMFAVerify},
		{"POST", "/api/v1/auth/mfa/webauthn/assertion/verify", ActionMFAVerify},
		{"POST", "/api/v1/auth/mfa/webauthn/assertion/start", ActionMFAChallenge},
	}
	for _, tc := range cases {
		spec, ok := rbac.LookupHTTPRoute(tc.method, tc.pattern)
		require.True(t, ok, "%s %s", tc.method, tc.pattern)
		name := spec.AuditName(nil)
		assert.Equal(t, tc.action, name.Action, "%s %s", tc.method, tc.pattern)
		assert.Equal(t, ActionScopeMFA, name.ResourceType, "%s %s", tc.method, tc.pattern)
	}
}
