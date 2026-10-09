package circleci

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	// ErrPipelineMismatch means the workflow's pipeline is not building the approved commit/repository.
	ErrPipelineMismatch = errors.New("CircleCI pipeline does not match the deploy approval")
	// ErrInvalidWorkflowID means the workflow ID is not a CircleCI UUID.
	ErrInvalidWorkflowID = errors.New("invalid CircleCI workflow_id")

	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ApprovalExpectation is what the pipeline behind a workflow must match before its hold job is approved.
type ApprovalExpectation struct {
	// Revision is the full commit SHA the deploy approval was granted for (required).
	Revision string
	// Repo is the app's "owner/repo" VCS repository. When empty, only the revision is checked.
	Repo string
}

// ValidateWorkflowID reports an error unless workflowID is a CircleCI workflow UUID.
func ValidateWorkflowID(workflowID string) error {
	if !uuidPattern.MatchString(workflowID) {
		return fmt.Errorf("%w: %q", ErrInvalidWorkflowID, workflowID)
	}
	return nil
}

type pipelineDetails struct {
	ID          string `json:"id"`
	ProjectSlug string `json:"project_slug"`
	VCS         struct {
		Revision            string `json:"revision"`
		TargetRepositoryURL string `json:"target_repository_url"`
		OriginRepositoryURL string `json:"origin_repository_url"`
	} `json:"vcs"`
}

func (c *Client) getPipeline(pipelineID string) (*pipelineDetails, error) {
	body, err := c.doRequest(http.MethodGet, fmt.Sprintf("%s/pipeline/%s", c.BaseURL, url.PathEscape(pipelineID)))
	if err != nil {
		return nil, err
	}
	var details pipelineDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline: %w", err)
	}
	return &details, nil
}

// verifyPipeline checks that a pipeline builds the expected revision of the expected repository,
// and is not a pipeline for a fork's code.
func verifyPipeline(pipeline *pipelineDetails, expect ApprovalExpectation) error {
	revision := strings.TrimSpace(expect.Revision)
	if revision == "" {
		return fmt.Errorf("%w: no approved revision", ErrPipelineMismatch)
	}
	if !strings.EqualFold(strings.TrimSpace(pipeline.VCS.Revision), revision) {
		return fmt.Errorf("%w: pipeline revision %q, approved %q", ErrPipelineMismatch, pipeline.VCS.Revision, revision)
	}

	target := repoFromURL(pipeline.VCS.TargetRepositoryURL)
	origin := repoFromURL(pipeline.VCS.OriginRepositoryURL)
	if origin != "" && target != "" && !strings.EqualFold(origin, target) {
		return fmt.Errorf("%w: pipeline builds code from fork %s", ErrPipelineMismatch, origin)
	}

	expectedRepo := strings.TrimSpace(expect.Repo)
	if expectedRepo == "" {
		return nil
	}
	actual := target
	if actual == "" {
		actual = repoFromProjectSlug(pipeline.ProjectSlug)
	}
	if actual == "" {
		return fmt.Errorf("%w: cannot determine the pipeline's repository (expected %s)",
			ErrPipelineMismatch, expectedRepo)
	}
	if !strings.EqualFold(actual, expectedRepo) {
		return fmt.Errorf("%w: pipeline repository %s, expected %s", ErrPipelineMismatch, actual, expectedRepo)
	}
	return nil
}

// repoFromURL returns "owner/repo" for a repository URL like https://github.com/owner/repo(.git).
func repoFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// repoFromProjectSlug returns "owner/repo" for VCS-style project slugs ("gh/owner/repo").
// GitHub App projects ("circleci/<org-id>/<project-id>") don't name the repository.
func repoFromProjectSlug(slug string) string {
	parts := strings.Split(strings.TrimSpace(slug), "/")
	if len(parts) != 3 {
		return ""
	}
	switch parts[0] {
	case "gh", "github", "bb", "bitbucket":
		return parts[1] + "/" + parts[2]
	default:
		return ""
	}
}
