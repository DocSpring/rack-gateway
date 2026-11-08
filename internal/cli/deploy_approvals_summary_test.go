package cli

import (
	"strings"
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

// The branch is what the person approving recognizes, so it comes first. A run's later touches have no PIN dialog.
func TestApprovalTouchNamesTheBranchFirst(t *testing.T) {
	req := &deployApprovalRequest{PublicID: "req-1", App: "api", GitBranch: "feature/fix-login"}
	require.Equal(t, "approve feature/fix-login (api on staging, 2 of 4)", approvalTouch("staging", req, 2, 4))
	require.Equal(t, "approve feature/fix-login (api on staging)", approvalTouch("staging", req, 0, 0))

	req.GitBranch = "  "
	require.Equal(t, "approve api on staging", approvalTouch("staging", req, 0, 0), "a blank branch is left out")

	req.GitBranch = "fix\x1b[2J"
	require.Equal(t, "approve fix?[2J (api on staging)", approvalTouch("staging", req, 0, 0))
}

func TestApprovalSummaryLineNamesTheBranchFirst(t *testing.T) {
	req := &deployApprovalRequest{
		PublicID: "req-1", App: "api", GitBranch: "feature/fix-login", GitCommitHash: appsTestCommit,
		Message: "Fix login",
	}
	require.Equal(t,
		`feature/fix-login: api on staging, commit 36a302d, "Fix login"`, approvalSummaryLine("staging", req))

	req.GitBranch = ""
	require.Equal(t, `api on staging, commit 36a302d, "Fix login"`, approvalSummaryLine("staging", req))
}

// The PIN dialog cuts each line at 160 characters. A long branch is cut first, so the app and rack still show.
func TestApprovalSummaryLineCutsALongBranch(t *testing.T) {
	branch := strings.Repeat("b", 300)
	req := &deployApprovalRequest{App: "api", GitBranch: branch, GitCommitHash: appsTestCommit, Message: "Fix"}

	line := approvalSummaryLine("staging", req)
	require.True(t, strings.HasPrefix(line, strings.Repeat("b", maxBranchRunes)+"...: api on staging, commit 36a302d"))
	require.Less(t, len([]rune(line)), 160)
	require.Contains(t, approvalTouch("staging", req, 0, 0), "(api on staging)")
}
