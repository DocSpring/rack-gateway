package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

const defaultAPIBaseURL = "https://api.github.com"

// Client handles GitHub API requests
type Client struct {
	token      string
	apiBaseURL string
	httpClient *http.Client
}

// NewClient creates a new GitHub API client
func NewClient(token string) *Client {
	return &Client{
		token:      token,
		apiBaseURL: defaultAPIBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// repoURL builds an API URL under /repos/{owner}/{repo}, path-escaping every segment.
func (c *Client) repoURL(owner, repo string, segments ...string) string {
	parts := make([]string, 0, 4+len(segments))
	parts = append(parts, c.apiBaseURL, "repos", url.PathEscape(owner), url.PathEscape(repo))
	for _, segment := range segments {
		parts = append(parts, url.PathEscape(segment))
	}
	return strings.Join(parts, "/")
}

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	State string `json:"state"`
}

// PRCommentRequest represents a GitHub PR comment request
type PRCommentRequest struct {
	Body string `json:"body"`
}

// Branch represents a GitHub branch
type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
		URL string `json:"url"`
	} `json:"commit"`
}

// SplitRepo splits a "owner/repo" string into owner and repo parts.
// Returns empty strings if the format is invalid.
func SplitRepo(ownerRepo string) (owner, repo string) {
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// ExtractPRNumber extracts the PR number from a GitHub PR URL.
// Example: "https://github.com/owner/repo/pull/123" returns 123
func ExtractPRNumber(prURL string) (int, error) {
	// Expected format: https://github.com/owner/repo/pull/123
	parts := strings.Split(prURL, "/")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid PR URL format")
	}

	// Get the last part which should be the number
	prNumStr := parts[len(parts)-1]
	prNum, err := strconv.Atoi(prNumStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse PR number: %w", err)
	}

	return prNum, nil
}

// VerifyCommitOptions holds options for commit verification
type VerifyCommitOptions struct {
	RequirePR bool   // Whether a pull request is required
	Mode      string // Verification mode: settings.VerifyGitCommitModeBranch or settings.VerifyGitCommitModeLatest
}

// doRequest executes an HTTP request to the GitHub API with standard headers and error handling.
// Parameters:
//   - method: HTTP method (GET, POST, etc.)
//   - apiURL: Full API URL
//   - body: Optional request body (can be nil)
//   - expectedStatus: Expected HTTP status code(s) for success
//   - notFoundError: Optional custom error message for 404 responses (if empty, 404 is treated as a regular error)
//   - target: Optional pointer to decode JSON response into (can be nil)
//
// Returns an error if the request fails, returns unexpected status, or JSON decoding fails.
func (c *Client) doRequest(
	method, apiURL string,
	body io.Reader,
	expectedStatus int,
	notFoundError string,
	target interface{},
) error {
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle 404 with custom error if provided
	if resp.StatusCode == http.StatusNotFound && notFoundError != "" {
		return fmt.Errorf("%s", notFoundError)
	}

	// Check for expected status
	if resp.StatusCode != expectedStatus {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Decode JSON response if target is provided
	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// VerifyCommitAndFindPR verifies a commit against GitHub and optionally finds a PR.
// The behavior depends on the options:
// - Mode "latest": commit must be the current head of the specified branch
// - Mode "branch": commit must be on the specified branch (the head or one of its ancestors)
// - RequirePR: if true, requires an open PR for the branch
// commitHash must be a full 40-character SHA. Returns the PR URL if found (empty string if not
// required/found), or an error.
func (c *Client) VerifyCommitAndFindPR(
	owner, repo, branch, commitHash string,
	opts VerifyCommitOptions,
) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("GitHub token not configured")
	}

	branchInfo, err := c.getBranch(owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("failed to get branch info: %w", err)
	}

	if err := c.verifyCommitForMode(owner, repo, branch, commitHash, branchInfo.Commit.SHA, opts.Mode); err != nil {
		return "", err
	}

	// Always look up the PR (for informational purposes)
	pr, err := c.findPRForBranch(owner, repo, branch)
	if err != nil {
		// Don't fail on PR lookup errors if PR is not required
		if opts.RequirePR {
			return "", fmt.Errorf("failed to find PR for branch: %w", err)
		}
		return "", nil
	}

	if opts.RequirePR && pr == nil {
		return "", fmt.Errorf("no open pull request found for branch %s", branch)
	}
	if pr != nil {
		return pr.HTMLURL, nil
	}
	return "", nil
}

func (c *Client) verifyCommitForMode(owner, repo, branch, commitHash, branchHeadSHA, mode string) error {
	switch mode {
	case settings.VerifyGitCommitModeLatest:
		if !strings.EqualFold(strings.TrimSpace(branchHeadSHA), strings.TrimSpace(commitHash)) {
			return fmt.Errorf(
				"commit %s is not the latest commit on branch %s (latest: %s)",
				commitHash,
				branch,
				branchHeadSHA,
			)
		}
		return nil
	case settings.VerifyGitCommitModeBranch:
		return c.verifyCommitOnBranch(owner, repo, branch, commitHash, branchHeadSHA)
	default:
		return fmt.Errorf(
			"invalid verify_git_commit_mode: %s (must be '%s' or '%s')",
			mode,
			settings.VerifyGitCommitModeBranch,
			settings.VerifyGitCommitModeLatest,
		)
	}
}

// compareResponse is the subset of GitHub's compare API response used to check ancestry.
type compareResponse struct {
	Status string `json:"status"` // "ahead", "behind", "identical" or "diverged"
}

// verifyCommitOnBranch verifies that a commit is on a branch: it must be the branch head or one of
// its ancestors. GET /repos/{owner}/{repo}/compare/{head}...{commit} reports the commit as "behind"
// (ancestor) or "identical" (head) in that case; "ahead" or "diverged" means it is not on the branch.
// The API returns 200 for any two commits in the repository, so the status must be checked.
func (c *Client) verifyCommitOnBranch(owner, repo, branch, commitHash, branchHeadSHA string) error {
	apiURL := c.repoURL(owner, repo, "compare", branchHeadSHA+"..."+commitHash)

	var result compareResponse
	notFoundError := fmt.Sprintf("commit %s not found or not on branch %s", commitHash, branch)
	if err := c.doRequest("GET", apiURL, nil, http.StatusOK, notFoundError, &result); err != nil {
		return err
	}
	switch result.Status {
	case "behind", "identical":
		return nil
	default:
		return fmt.Errorf("commit %s is not on branch %s (compare status: %q)", commitHash, branch, result.Status)
	}
}

// getBranch fetches branch information from GitHub
func (c *Client) getBranch(owner, repo, branch string) (*Branch, error) {
	apiURL := c.repoURL(owner, repo, "branches", branch)

	var branchInfo Branch
	notFoundError := fmt.Sprintf("branch %s not found in repository %s/%s", branch, owner, repo)
	if err := c.doRequest("GET", apiURL, nil, http.StatusOK, notFoundError, &branchInfo); err != nil {
		return nil, err
	}

	return &branchInfo, nil
}

// findPRForBranch finds an open PR for the specified branch
func (c *Client) findPRForBranch(owner, repo, branch string) (*PullRequest, error) {
	query := url.Values{"head": {owner + ":" + branch}, "state": {"open"}}
	apiURL := c.repoURL(owner, repo, "pulls") + "?" + query.Encode()

	var prs []PullRequest
	if err := c.doRequest("GET", apiURL, nil, http.StatusOK, "", &prs); err != nil {
		return nil, err
	}

	if len(prs) == 0 {
		return nil, nil
	}

	return &prs[0], nil
}

// PostPRComment posts a comment on a pull request
func (c *Client) PostPRComment(owner, repo string, prNumber int, comment string) error {
	apiURL := c.repoURL(owner, repo, "issues", strconv.Itoa(prNumber), "comments")

	body, err := json.Marshal(PRCommentRequest{Body: comment})
	if err != nil {
		return fmt.Errorf("failed to marshal comment: %w", err)
	}

	return c.doRequest("POST", apiURL, strings.NewReader(string(body)), http.StatusCreated, "", nil)
}
