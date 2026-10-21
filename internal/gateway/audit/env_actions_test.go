package audit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

func TestEnvChangeActionOnlyCallsARemovedKeyAnUnset(t *testing.T) {
	cases := []struct {
		diff         envutil.EnvDiff
		action, kind string
	}{
		{envutil.EnvDiff{Key: "PORT", NewVal: "4000"}, "env.set", "env"},
		{envutil.EnvDiff{Key: "PORT", OldVal: "3000"}, "env.set", "env"},
		{envutil.EnvDiff{Key: "PORT", OldVal: "3000", NewVal: "  "}, "env.set", "env"},
		{envutil.EnvDiff{Key: "PORT", OldVal: "3000", Removed: true}, "env.unset", "env"},
		{envutil.EnvDiff{Key: "API_TOKEN", OldVal: "a", Secret: true}, "secret.set", "secret"},
		{envutil.EnvDiff{Key: "API_TOKEN", OldVal: "a", Secret: true, Removed: true}, "secret.unset", "secret"},
		{envutil.EnvDiff{Key: "DB_URL", OldVal: "a", Protected: true}, "env.set_protected", "env"},
		{envutil.EnvDiff{Key: "DB_URL", OldVal: "a", Protected: true, Removed: true}, "env.unset_protected", "env"},
	}
	for _, tc := range cases {
		action, kind := EnvChangeAction(tc.diff)
		require.Equal(t, tc.action, action, "%+v", tc.diff)
		require.Equal(t, tc.kind, kind, "%+v", tc.diff)
	}
}
