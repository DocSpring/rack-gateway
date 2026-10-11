package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/github"
)

// PostPRCommentArgs contains parameters for posting GitHub PR comments.
// The GitHub token is held by the worker and never stored in job arguments.
type PostPRCommentArgs struct {
	Owner                   string `json:"owner"`
	Repo                    string `json:"repo"`
	PRNumber                int    `json:"pr_number"`
	Comment                 string `json:"comment"`
	DeployApprovalRequestID int64  `json:"deploy_approval_request_id"`
}

// Kind returns the unique identifier for this job type
func (PostPRCommentArgs) Kind() string { return "github:post_pr_comment" }

// PostPRCommentWorker posts comments to GitHub pull requests
type PostPRCommentWorker struct {
	river.WorkerDefaults[PostPRCommentArgs]
	token      string
	apiBaseURL string
}

// NewPostPRCommentWorker creates a GitHub PR comment worker using the given API token and API base URL ("" for
// api.github.com).
func NewPostPRCommentWorker(token, apiBaseURL string) *PostPRCommentWorker {
	return &PostPRCommentWorker{token: token, apiBaseURL: apiBaseURL}
}

// Work posts the PR comment
func (w *PostPRCommentWorker) Work(_ context.Context, job *river.Job[PostPRCommentArgs]) error {
	if strings.TrimSpace(w.token) == "" {
		return river.JobCancel(fmt.Errorf("GitHub token not configured"))
	}
	args := job.Args
	client := github.NewClient(w.token, w.apiBaseURL)

	if err := client.PostPRComment(args.Owner, args.Repo, args.PRNumber, args.Comment); err != nil {
		return fmt.Errorf("failed to post GitHub PR comment: %w", err)
	}

	return nil
}
