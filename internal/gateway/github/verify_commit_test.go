package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

const (
	branchHead = "1111111111111111111111111111111111111111"
	deployed   = "2222222222222222222222222222222222222222"
)

// newVerifyServer fakes the GitHub branches and compare APIs. compareStatus is what the compare
// endpoint reports for {branchHead}...{deployed}.
func newVerifyServer(t *testing.T, compareStatus string) (*Client, *[]string) {
	t.Helper()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		switch {
		case r.URL.EscapedPath() == "/repos/DocSpring/docspring/branches/feature%2Fdeploy":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"commit": map[string]string{"sha": branchHead}})
		case strings.HasPrefix(r.URL.Path, "/repos/DocSpring/docspring/compare/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": compareStatus})
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.URL.Query().Get("head") == "DocSpring:feature/deploy":
			prs := []map[string]string{{"html_url": "https://github.com/DocSpring/docspring/pull/7"}}
			_ = json.NewEncoder(w).Encode(prs)
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient("token")
	client.apiBaseURL = server.URL
	return client, &paths
}

func TestVerifyCommitOnBranchChecksCompareStatus(t *testing.T) {
	opts := VerifyCommitOptions{Mode: settings.VerifyGitCommitModeBranch}
	for status, wantOK := range map[string]bool{
		"behind":    true, // deployed commit is an ancestor of the branch head
		"identical": true, // deployed commit is the branch head
		"ahead":     false,
		"diverged":  false, // e.g. a commit from another branch or a fork
	} {
		client, paths := newVerifyServer(t, status)
		_, err := client.VerifyCommitAndFindPR("DocSpring", "docspring", "feature/deploy", deployed, opts)
		if wantOK {
			require.NoErrorf(t, err, "status %s", status)
		} else {
			require.ErrorContainsf(t, err, "is not on branch", "status %s", status)
		}
		require.Contains(t, *paths, "/repos/DocSpring/docspring/compare/"+branchHead+"..."+deployed)
	}
}

func TestVerifyLatestCommitRequiresExactSHA(t *testing.T) {
	opts := VerifyCommitOptions{Mode: settings.VerifyGitCommitModeLatest}
	client, _ := newVerifyServer(t, "identical")

	_, err := client.VerifyCommitAndFindPR("DocSpring", "docspring", "feature/deploy", branchHead, opts)
	require.NoError(t, err)

	for _, commit := range []string{branchHead[:7], "1", deployed} {
		_, err = client.VerifyCommitAndFindPR("DocSpring", "docspring", "feature/deploy", commit, opts)
		require.ErrorContainsf(t, err, "is not the latest commit", "commit %q", commit)
	}
}

func TestBranchNamesArePathEscaped(t *testing.T) {
	client, paths := newVerifyServer(t, "behind")
	prURL, err := client.VerifyCommitAndFindPR(
		"DocSpring", "docspring", "feature/deploy", deployed,
		VerifyCommitOptions{Mode: settings.VerifyGitCommitModeBranch, RequirePR: true},
	)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/DocSpring/docspring/pull/7", prURL)
	require.Contains(t, *paths, "/repos/DocSpring/docspring/branches/feature%2Fdeploy")

	_, err = client.VerifyCommitAndFindPR(
		"DocSpring", "docspring", "../../other/repo", deployed,
		VerifyCommitOptions{Mode: settings.VerifyGitCommitModeBranch},
	)
	require.Error(t, err, "path traversal in a branch name must not reach another endpoint")
}
