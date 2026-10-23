package circleci

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrPipelineMismatch means the workflow's pipeline is not building the approved commit/repository.
	ErrPipelineMismatch = errors.New("CircleCI pipeline does not match the deploy approval")
	// ErrInvalidWorkflowID means the workflow ID is not a CircleCI UUID.
	ErrInvalidWorkflowID = errors.New("invalid CircleCI workflow_id")

	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// vcsHosts maps the VCS providers an app's vcs_provider setting can name, and the providers in CircleCI's VCS-style
// project slugs, to their hosts.
var vcsHosts = map[string]string{
	"github": "github.com", "gh": "github.com",
	"bitbucket": "bitbucket.org", "bb": "bitbucket.org",
}

// ApprovalExpectation is what the pipeline behind a workflow must match before its hold job is approved.
type ApprovalExpectation struct {
	// Revision is the full commit SHA the deploy approval was granted for (required).
	Revision string
	// Repo is the app's repository as "host/owner/repo" (RepoIdentity), required. A repository with the same
	// owner/repo on another host is a different repository.
	Repo string
	// PipelineNumber is the pipeline number the requester gave, which the approver was shown (required). The
	// workflow must belong to that pipeline, so the hold job approved is the one in the pipeline the approver saw.
	PipelineNumber string
}

// RepoIdentity returns "host/owner/repo" for an app's vcs_provider and vcs_repo ("owner/repo") settings.
func RepoIdentity(provider, repo string) (string, error) {
	host, ok := vcsHosts[strings.ToLower(strings.TrimSpace(provider))]
	if !ok {
		return "", fmt.Errorf("unsupported VCS provider %q", provider)
	}
	parts := strings.Split(strings.TrimSpace(repo), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("vcs_repo %q must be owner/repo", repo)
	}
	return host + "/" + parts[0] + "/" + parts[1], nil
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
	Number      int64  `json:"number"`
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

// verifyPipeline checks that a pipeline is the one the approver was shown, builds the expected revision of the
// expected repository, and is not a pipeline for a fork's code.
func verifyPipeline(pipeline *pipelineDetails, expect ApprovalExpectation) error {
	revision := strings.TrimSpace(expect.Revision)
	if revision == "" {
		return fmt.Errorf("%w: no approved revision", ErrPipelineMismatch)
	}
	if !strings.EqualFold(strings.TrimSpace(pipeline.VCS.Revision), revision) {
		return fmt.Errorf("%w: pipeline revision %q, approved %q", ErrPipelineMismatch, pipeline.VCS.Revision, revision)
	}
	number := strings.TrimSpace(expect.PipelineNumber)
	if number == "" || number != strconv.FormatInt(pipeline.Number, 10) {
		return fmt.Errorf("%w: the workflow is in pipeline %d, the request named pipeline %q",
			ErrPipelineMismatch, pipeline.Number, expect.PipelineNumber)
	}

	return checkPipelineRepository(pipeline, expect.Repo)
}

// checkPipelineRepository checks that the pipeline builds expectedRepo's own code: its repository (the target
// repository URL, else the project slug) is expectedRepo, and its origin repository, when CircleCI gives one, is the
// same repository rather than a fork. A repository URL that's given but can't be read is refused.
func checkPipelineRepository(pipeline *pipelineDetails, expectedRepo string) error {
	expectedRepo = strings.TrimSpace(expectedRepo)
	if expectedRepo == "" {
		return fmt.Errorf("%w: the app has no repository (vcs_repo) to check the pipeline against", ErrPipelineMismatch)
	}
	target, err := pipelineRepoURL("target", pipeline.VCS.TargetRepositoryURL)
	if err != nil {
		return err
	}
	origin, err := pipelineRepoURL("origin", pipeline.VCS.OriginRepositoryURL)
	if err != nil {
		return err
	}
	actual := target
	if actual == "" {
		actual = repoIdentityFromProjectSlug(pipeline.ProjectSlug)
	}
	if actual == "" {
		return fmt.Errorf("%w: cannot determine the pipeline's repository (expected %s)",
			ErrPipelineMismatch, expectedRepo)
	}
	if !strings.EqualFold(actual, expectedRepo) {
		return fmt.Errorf("%w: pipeline repository %s, expected %s", ErrPipelineMismatch, actual, expectedRepo)
	}
	if origin != "" && !strings.EqualFold(origin, actual) {
		return fmt.Errorf("%w: pipeline builds code from fork %s", ErrPipelineMismatch, origin)
	}
	return nil
}

// pipelineRepoURL returns the repository a pipeline's repository URL (role is "target" or "origin") names, "" when
// there's none, or an error when it can't be read.
func pipelineRepoURL(role, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	identity := repoIdentityFromURL(raw)
	if identity == "" {
		return "", fmt.Errorf("%w: can't read the pipeline's %s repository URL %q", ErrPipelineMismatch, role, raw)
	}
	return identity, nil
}

// repoIdentityFromURL returns "host/owner/repo" for a repository URL like https://github.com/owner/repo(.git).
func repoIdentityFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname()) + "/" + parts[0] + "/" + parts[1]
}

// repoIdentityFromProjectSlug returns "host/owner/repo" for VCS-style project slugs ("gh/owner/repo").
// GitHub App projects ("circleci/<org-id>/<project-id>") don't name the repository.
func repoIdentityFromProjectSlug(slug string) string {
	parts := strings.Split(strings.TrimSpace(slug), "/")
	if len(parts) != 3 {
		return ""
	}
	host, ok := vcsHosts[parts[0]]
	if !ok {
		return ""
	}
	return host + "/" + parts[1] + "/" + parts[2]
}
