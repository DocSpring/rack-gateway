package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApprovalTouch(t *testing.T) {
	req := &deployApprovalRequest{PublicID: "req-1", App: "api"}
	require.Equal(t, "approve api on staging (2 of 4)", approvalTouch("staging", req, 2, 4))
	require.Equal(t, "approve api on staging", approvalTouch("staging", req, 1, 1))
	require.Equal(t, "approve api on staging", approvalTouch("staging", req, 0, 0), "the total isn't known")

	req.App = ""
	require.Equal(t, "approve request req-1 on us", approvalTouch("us", req, 0, 0))

	req.App = "api\x1b[2J"
	require.NotContains(t, approvalTouch("us", req, 0, 0), "\x1b", "control characters are removed")
}
