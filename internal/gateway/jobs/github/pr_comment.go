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
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
	Comment  string `json:"comment"`
	// Marker identifies the comment (see DeployApprovalMarker). It's added to the comment, and a retry posts the
	// comment only when no comment on the pull request has it: an attempt whose response was lost may have posted.
	Marker string `json:"marker"`
}

// DeployApprovalMarker is the marker of the comment about the deploy approval request with publicID: an HTML comment,
// which GitHub doesn't show.
func DeployApprovalMarker(publicID string) string {
	return "<!-- rack-gateway deploy approval request " + publicID + " -->"
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

// alreadyPosted reports whether an earlier attempt of job posted its comment (one with its marker is on the pull
// request). The first attempt doesn't look.
func alreadyPosted(client *github.Client, job *river.Job[PostPRCommentArgs]) (bool, error) {
	if job.Attempt <= 1 || job.Args.Marker == "" {
		return false, nil
	}
	return client.PRCommentExists(job.Args.Owner, job.Args.Repo, job.Args.PRNumber, job.Args.Marker)
}

// Work posts the PR comment, unless an earlier attempt posted it.
func (w *PostPRCommentWorker) Work(_ context.Context, job *river.Job[PostPRCommentArgs]) error {
	if strings.TrimSpace(w.token) == "" {
		return river.JobCancel(fmt.Errorf("GitHub token not configured"))
	}
	args := job.Args
	client := github.NewClient(w.token, w.apiBaseURL)

	posted, err := alreadyPosted(client, job)
	if err != nil {
		return fmt.Errorf("failed to check for the GitHub PR comment: %w", err)
	}
	if posted {
		return nil
	}

	comment := args.Comment
	if args.Marker != "" {
		comment += "\n\n" + args.Marker
	}
	if err := client.PostPRComment(args.Owner, args.Repo, args.PRNumber, comment); err != nil {
		return fmt.Errorf("failed to post GitHub PR comment: %w", err)
	}

	return nil
}
