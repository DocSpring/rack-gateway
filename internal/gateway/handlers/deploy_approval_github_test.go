package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

const (
	ghBranchHead = "1111111111111111111111111111111111111111"
	ghBehind     = "2222222222222222222222222222222222222222" // an older commit on the branch
	ghElsewhere  = "3333333333333333333333333333333333333333" // a commit that isn't on the branch
	ghPRURL      = "https://github.com/acme/web/pull/7"
)

// fakeGitHub serves the branches, compare and pulls APIs for acme/web, for the token "gh-token". withPR controls
// whether the branch has an open pull request.
func fakeGitHub(t *testing.T, withPR bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		switch {
		case r.Header.Get("Authorization") != "Bearer gh-token":
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasPrefix(path, "/repos/acme/web/branches/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"commit": map[string]string{"sha": ghBranchHead}})
		case strings.HasPrefix(path, "/repos/acme/web/compare/"):
			status := "behind"
			switch {
			case strings.HasSuffix(path, "..."+ghBranchHead):
				status = "identical"
			case strings.HasSuffix(path, "..."+ghElsewhere):
				status = "diverged"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
		case strings.HasSuffix(path, "/pulls") && withPR:
			_ = json.NewEncoder(w).Encode([]map[string]string{{"html_url": ghPRURL}})
		case strings.HasSuffix(path, "/pulls"):
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// newVerifyingFixture is a create fixture with GitHub verification set up for app "myapp" (repo acme/web).
func newVerifyingFixture(t *testing.T, withPR bool, appSettings map[string]interface{}) *createApprovalFixture {
	t.Helper()
	f := newCreateApprovalFixture(t)
	f.handler.settingsService = settings.NewService(f.handler.database)
	f.handler.config.GitHubToken = "gh-token"
	f.handler.config.GitHubAPIURL = fakeGitHub(t, withPR).URL
	values := map[string]interface{}{settings.KeyVCSRepo: "acme/web"}
	for key, value := range appSettings {
		values[key] = value
	}
	for key, value := range values {
		require.NoError(t, f.handler.settingsService.SetAppSetting("myapp", key, value, nil))
	}
	return f
}

func requireNoRequestCreated(t *testing.T, f *createApprovalFixture) {
	t.Helper()
	requests, err := f.handler.database.ListDeployApprovalRequests(db.DeployApprovalRequestListOptions{})
	require.NoError(t, err)
	require.Empty(t, requests, "a failed check must not create a request")
}

func responseError(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func TestGitHubVerificationRequiresPRForBranch(t *testing.T) {
	f := newVerifyingFixture(t, false, nil)
	resp := f.create(t, ghBranchHead)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, responseError(t, resp))
	requireNoRequestCreated(t, f)

	f = newVerifyingFixture(t, false, map[string]interface{}{settings.KeyRequirePRForBranch: false})
	resp = f.create(t, ghBranchHead)
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))
}

func TestGitHubVerificationStoresThePRURL(t *testing.T) {
	f := newVerifyingFixture(t, true, nil)
	resp := f.create(t, ghBranchHead)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var got DeployApprovalRequestResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, ghPRURL, got.PrURL)
}

func TestGitHubVerificationBlocksTheDefaultBranch(t *testing.T) {
	f := newVerifyingFixture(t, true, nil)
	resp := f.createOnBranch(t, ghBranchHead, "main")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, responseError(t, resp), "default branch 'main'")
	requireNoRequestCreated(t, f)

	f = newVerifyingFixture(t, true, map[string]interface{}{settings.KeyDefaultBranch: "feature/deploy"})
	resp = f.create(t, ghBranchHead)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "default_branch is honored")

	f = newVerifyingFixture(t, true, map[string]interface{}{settings.KeyAllowDeployFromDefaultBranch: true})
	resp = f.createOnBranch(t, ghBranchHead, "main")
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))
}

func TestGitHubVerificationCommitModes(t *testing.T) {
	latest := map[string]interface{}{settings.KeyVerifyGitCommitMode: settings.VerifyGitCommitModeLatest}
	branch := map[string]interface{}{settings.KeyVerifyGitCommitMode: settings.VerifyGitCommitModeBranch}

	resp := newVerifyingFixture(t, true, latest).create(t, ghBehind)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "latest mode rejects a commit behind the head")

	resp = newVerifyingFixture(t, true, branch).create(t, ghBehind)
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))

	f := newVerifyingFixture(t, true, branch)
	resp = f.create(t, ghElsewhere)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "branch mode rejects a commit that isn't on the branch")
	requireNoRequestCreated(t, f)
}

// With verification on, a missing vcs_repo or GITHUB_TOKEN refuses the request instead of skipping the checks.
func TestGitHubVerificationFailsClosedWithoutConfig(t *testing.T) {
	f := newVerifyingFixture(t, true, map[string]interface{}{settings.KeyVCSRepo: ""})
	resp := f.create(t, ghBranchHead)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, responseError(t, resp), "the app has no vcs_repo setting")
	requireNoRequestCreated(t, f)

	f = newVerifyingFixture(t, true, nil)
	f.handler.config.GitHubToken = ""
	resp = f.create(t, ghBranchHead)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, responseError(t, resp), "the gateway has no GITHUB_TOKEN")
	requireNoRequestCreated(t, f)

	f = newVerifyingFixture(t, true, map[string]interface{}{settings.KeyGitHubVerification: false})
	f.handler.config.GitHubToken = ""
	resp = f.create(t, ghBranchHead)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "verification off needs neither")
}

// A PR comment names the app, rack and short commit, so comments for several apps or racks differ.
func TestDeployApprovalPRCommentNamesAppAndCommit(t *testing.T) {
	comment := deployApprovalPRComment("api", "staging", ghBranchHead, "https://gw/app/deploy-approval-requests/x")
	require.Contains(t, comment, "api on staging")
	require.Contains(t, comment, "`1111111`")
	require.Contains(t, comment, "https://gw/app/deploy-approval-requests/x")
	require.NotEqual(t, comment, deployApprovalPRComment("web", "staging", ghBranchHead, "https://gw/x"))
}
