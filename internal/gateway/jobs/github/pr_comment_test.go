package github

import (
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
		GitHubToken:             "ghp_secret",
		DeployApprovalRequestID: 456,
	}
	assert.Equal(t, "github:post_pr_comment", args.Kind())
}

// Test NewPostPRCommentWorker
func TestNewPostPRCommentWorker(t *testing.T) {
	worker := NewPostPRCommentWorker()
	require.NotNil(t, worker)
}
