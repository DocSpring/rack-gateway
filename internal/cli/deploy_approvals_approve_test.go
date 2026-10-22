package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
)

func TestApprovingWithAPITokenFailsBeforeCallingTheGateway(t *testing.T) {
	t.Setenv("RACK_GATEWAY_API_TOKEN", "rgw_token")
	_, _, err := getDeployApprovalMFAAuth(&cobra.Command{}, "staging", "", webauthn.Prompt{})
	require.ErrorIs(t, err, errAPITokenCannotApprove)
}

func TestTestAuthRejectsMFAModesForAPITokens(t *testing.T) {
	err := testAPITokenAuth("staging", "mfa")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unset RACK_GATEWAY_API_TOKEN")
	require.Contains(t, err.Error(), "drop --api-token")
}
