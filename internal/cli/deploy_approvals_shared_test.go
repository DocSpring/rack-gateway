package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// newGitRepo creates a repository with one empty commit and returns its directory and the commit's full SHA. It
// uses plumbing commands, so no hooks or signing configured for the user run.
func newGitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{
			"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false",
		}, args...)...)
		out, err := cmd.Output()
		require.NoError(t, err, "git %s", strings.Join(args, " "))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	commit := git("commit-tree", git("mktree"), "-m", "test")
	git("update-ref", "HEAD", commit)
	return dir, commit
}

// The gateway matches --commit as a prefix, so the commit taken from git must be the full SHA: a short one could
// also match another commit's request.
func TestCurrentCommitIsTheFullSHA(t *testing.T) {
	dir, head := newGitRepo(t)
	t.Chdir(dir)

	branch, commit, err := resolveBranchOrCommit("", "")
	require.NoError(t, err)
	require.Empty(t, branch)
	require.Equal(t, head, commit)
	require.Regexp(t, fullCommitSHA, commit)
}

// RACK_GATEWAY_URL names one gateway, so a command can't fan out to several racks with it: each rack's request
// (and, with a session, each rack's session token) would go to that one host.
func TestResolveRacksRefusesGatewayURLWithSeveralRacks(t *testing.T) {
	withCLIConfig(t, Config{Gateways: map[string]GatewayConfig{
		"us": {URL: "https://rack-gateway-us.example.com"},
		"eu": {URL: "https://rack-gateway-eu.example.com"},
	}})
	t.Setenv("RACK_GATEWAY_URL", "https://rack-gateway-us.example.com")

	for _, rackFlag := range []string{"all", "us,eu", "us, eu"} {
		RackFlag = rackFlag
		_, err := resolveRacks()
		require.ErrorIs(t, err, errGatewayURLWithManyRacks, rackFlag)
	}

	RackFlag = "us"
	racks, err := resolveRacks()
	require.NoError(t, err)
	require.Equal(t, []string{"us"}, racks)
}
