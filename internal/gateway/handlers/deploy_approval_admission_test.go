package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/ratelimit"
)

// A request that duplicates an open one is answered with it before GitHub verification calls GitHub, so repeating
// requests doesn't spend the gateway's GitHub quota.
func TestADuplicateApprovalRequestDoesntCallGitHub(t *testing.T) {
	f := newVerifyingFixture(t, true, nil)
	resp := f.create(t, ghBranchHead)
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))

	var calls atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(github.Close)
	f.handler.config.GitHubAPIURL = github.URL

	resp = f.create(t, ghBranchHead)
	require.Equal(t, http.StatusConflict, resp.StatusCode, responseError(t, resp))
	require.Zero(t, calls.Load())
}

// Each API token can create so many requests before it has to wait; duplicates don't count.
func TestApprovalRequestsAreLimitedPerCreator(t *testing.T) {
	f := newCreateApprovalFixture(t)
	f.handler.approvalCreations = approvalCreationLimit{creators: ratelimit.NewLimiter(1.0/3600, 2), retryAfter: "3600"}

	first := map[string]string{"app": "first", "git_commit_hash": ghBranchHead}
	resp := f.createWith(t, first)
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))
	resp = f.createWith(t, first)
	require.Equal(t, http.StatusConflict, resp.StatusCode, "a duplicate is answered with the open request")

	resp = f.createWith(t, map[string]string{"app": "second", "git_commit_hash": ghBranchHead})
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))

	resp = f.createWith(t, map[string]string{"app": "third", "git_commit_hash": ghBranchHead})
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, responseError(t, resp))
	require.Equal(t, "3600", resp.Header.Get("Retry-After"))
}
