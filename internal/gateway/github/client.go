package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// Client handles GitHub API requests
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a new GitHub API client
func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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

// VerifyCommitOptions holds options for commit verification
type VerifyCommitOptions struct {
	RequirePR bool   // Whether a pull request is required
	Mode      string // Verification mode: settings.VerifyGitCommitModeBranch or settings.VerifyGitCommitModeLatest
}

// VerifyCommitAndFindPR verifies a commit against GitHub and optionally finds a PR.
// The behavior depends on the options:
// - Mode "latest": commit must be the latest on the specified branch
// - Mode "branch": commit must exist on the specified branch (uses git compare API)
// - RequirePR: if true, requires an open PR for the branch
// Returns the PR URL if found (empty string if not required/found), or an error.
func (c *Client) VerifyCommitAndFindPR(owner, repo, branch, commitHash string, opts VerifyCommitOptions) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("GitHub token not configured")
	}

	// 1. Get the branch info
	branchInfo, err := c.getBranch(owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("failed to get branch info: %w", err)
	}

	// 2. Verify commit based on mode
	switch opts.Mode {
	case settings.VerifyGitCommitModeLatest:
		// Check if the commit hash matches the latest commit on the branch
		if !strings.HasPrefix(branchInfo.Commit.SHA, commitHash) && !strings.HasPrefix(commitHash, branchInfo.Commit.SHA) {
			return "", fmt.Errorf("commit %s is not the latest commit on branch %s (latest: %s)", commitHash, branch, branchInfo.Commit.SHA)
		}
	case settings.VerifyGitCommitModeBranch:
		// Verify commit exists on the branch using compare API
		if err := c.verifyCommitOnBranch(owner, repo, branch, commitHash, branchInfo.Commit.SHA); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("invalid verify_git_commit_mode: %s (must be '%s' or '%s')", opts.Mode, settings.VerifyGitCommitModeBranch, settings.VerifyGitCommitModeLatest)
	}

	// 3. Always look up the PR (for informational purposes)
	pr, err := c.findPRForBranch(owner, repo, branch)
	if err != nil {
		// Don't fail on PR lookup errors if PR is not required
		if opts.RequirePR {
			return "", fmt.Errorf("failed to find PR for branch: %w", err)
		}
		// Just log and continue if PR lookup fails but isn't required
		return "", nil
	}

	// If PR is required but not found, error
	if opts.RequirePR && pr == nil {
		return "", fmt.Errorf("no open pull request found for branch %s", branch)
	}

	// Return PR URL if found (empty string if not found)
	if pr != nil {
		return pr.HTMLURL, nil
	}

	return "", nil
}

// verifyCommitOnBranch verifies that a commit exists on a branch using the compare API
func (c *Client) verifyCommitOnBranch(owner, repo, branch, commitHash, branchHeadSHA string) error {
	// Use GitHub compare API to check if commit is an ancestor of the branch head
	// API: GET /repos/{owner}/{repo}/compare/{basehead}
	// If commit is on the branch, the compare will show it's behind or identical
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s", owner, repo, commitHash, branchHeadSHA)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create compare request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to compare commits: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("commit %s not found or not on branch %s", commitHash, branch)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub compare API returned status %d: %s", resp.StatusCode, string(body))
	}

	// If we get here, the commit exists and is reachable from the branch head
	return nil
}

// getBranch fetches branch information from GitHub
func (c *Client) getBranch(owner, repo, branch string) (*Branch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches/%s", owner, repo, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("branch %s not found in repository %s/%s", branch, owner, repo)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var branchInfo Branch
	if err := json.NewDecoder(resp.Body).Decode(&branchInfo); err != nil {
		return nil, fmt.Errorf("failed to decode branch response: %w", err)
	}

	return &branchInfo, nil
}

// findPRForBranch finds an open PR for the specified branch
func (c *Client) findPRForBranch(owner, repo, branch string) (*PullRequest, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?head=%s:%s&state=open", owner, repo, owner, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var prs []PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, fmt.Errorf("failed to decode PR list response: %w", err)
	}

	if len(prs) == 0 {
		return nil, nil
	}

	return &prs[0], nil
}
