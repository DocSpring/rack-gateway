package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePRComments is a GitHub API serving one pull request's comments, which records the comments posted to it.
type fakePRComments struct {
	mu       sync.Mutex
	comments []string
	posts    int
}

func (f *fakePRComments) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/o/r/issues/7/comments", r.URL.Path)
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodPost {
			var body struct {
				Body string `json:"body"`
			}
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NoError(t, json.Unmarshal(data, &body))
			f.comments = append(f.comments, body.Body)
			f.posts++
			w.WriteHeader(http.StatusCreated)
			return
		}
		listed := make([]map[string]string, 0, len(f.comments))
		for _, comment := range f.comments {
			listed = append(listed, map[string]string{"body": comment})
		}
		_ = json.NewEncoder(w).Encode(listed)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func commentJob(attempt int) *river.Job[PostPRCommentArgs] {
	return &river.Job[PostPRCommentArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt},
		Args: PostPRCommentArgs{
			Owner: "o", Repo: "r", PRNumber: 7, Comment: "Deploy approval requested",
			Marker: DeployApprovalMarker("dar_abc"),
		},
	}
}

// An attempt whose response was lost may have posted the comment. A retry posts it only when no comment has its
// marker.
func TestPostPRCommentRetryDoesNotPostTwice(t *testing.T) {
	fake := &fakePRComments{}
	worker := NewPostPRCommentWorker("token", fake.serve(t))

	require.NoError(t, worker.Work(context.Background(), commentJob(1)))
	require.Equal(t, []string{"Deploy approval requested\n\n<!-- rack-gateway deploy approval request dar_abc -->"},
		fake.comments)
	require.NoError(t, worker.Work(context.Background(), commentJob(2)))
	require.Equal(t, 1, fake.posts, "the retry found the comment")

	other := &fakePRComments{comments: []string{"an unrelated comment"}}
	worker = NewPostPRCommentWorker("token", other.serve(t))
	require.NoError(t, worker.Work(context.Background(), commentJob(2)))
	require.Equal(t, 1, other.posts, "a retry posts when no comment has the marker")
}

// Test PostPRCommentArgs.Kind
func TestPostPRCommentArgs_Kind(t *testing.T) {
	args := PostPRCommentArgs{
		Owner:    "myorg",
		Repo:     "myrepo",
		PRNumber: 123,
		Comment:  "Deployment approved",
		Marker:   DeployApprovalMarker("dar_123"),
	}
	assert.Equal(t, "github:post_pr_comment", args.Kind())
}

// The GitHub token must never be persisted in job arguments.
func TestPostPRCommentArgs_DoNotContainToken(t *testing.T) {
	encoded, err := json.Marshal(PostPRCommentArgs{Owner: "o", Repo: "r", PRNumber: 1, Comment: "c"})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "token")
}

// Test NewPostPRCommentWorker
func TestNewPostPRCommentWorker(t *testing.T) {
	worker := NewPostPRCommentWorker("token", "")
	require.NotNil(t, worker)
}
