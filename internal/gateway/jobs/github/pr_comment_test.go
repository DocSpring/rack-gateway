package github

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test PostPRCommentArgs.Kind
func TestPostPRCommentArgs_Kind(t *testing.T) {
	args := PostPRCommentArgs{
		Owner:                   "myorg",
		Repo:                    "myrepo",
		PRNumber:                123,
		Comment:                 "Deployment approved",
		DeployApprovalRequestID: 456,
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
	worker := NewPostPRCommentWorker("token")
	require.NotNil(t, worker)
}
