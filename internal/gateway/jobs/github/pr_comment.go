package github

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/github"
)

// PostPRCommentArgs contains parameters for posting GitHub PR comments
type PostPRCommentArgs struct {
	Owner                   string `json:"owner"`
	Repo                    string `json:"repo"`
	PRNumber                int    `json:"pr_number"`
	Comment                 string `json:"comment"`
	GitHubToken             string `json:"github_token"`
	DeployApprovalRequestID int64  `json:"deploy_approval_request_id"`
}

// Kind returns the unique identifier for this job type
func (PostPRCommentArgs) Kind() string { return "github:post_pr_comment" }

// PostPRCommentWorker posts comments to GitHub pull requests
type PostPRCommentWorker struct {
	river.WorkerDefaults[PostPRCommentArgs]
}

// NewPostPRCommentWorker creates a new GitHub PR comment worker
func NewPostPRCommentWorker() *PostPRCommentWorker {
	return &PostPRCommentWorker{}
}

// Work posts the PR comment
func (w *PostPRCommentWorker) Work(_ context.Context, job *river.Job[PostPRCommentArgs]) error {
	args := job.Args

	// Create client with token from job args
	client := github.NewClient(args.GitHubToken)

	if err := client.PostPRComment(args.Owner, args.Repo, args.PRNumber, args.Comment); err != nil {
		return fmt.Errorf("failed to post GitHub PR comment: %w", err)
	}

	return nil
}
