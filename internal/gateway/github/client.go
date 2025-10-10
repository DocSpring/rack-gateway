package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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

// VerifyCommitAndFindPR verifies that:
// 1. The commit exists on the specified branch
// 2. The commit is the latest commit on that branch
// 3. There is an open PR for that branch
// Returns the PR URL if all checks pass, or an error describing the failure.
func (c *Client) VerifyCommitAndFindPR(owner, repo, branch, commitHash string) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("GitHub token not configured")
	}

	// 1. Get the branch and verify the commit is the latest
	branchInfo, err := c.getBranch(owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("failed to get branch info: %w", err)
	}

	// Check if the commit hash matches the latest commit on the branch
	if !strings.HasPrefix(branchInfo.Commit.SHA, commitHash) && !strings.HasPrefix(commitHash, branchInfo.Commit.SHA) {
		return "", fmt.Errorf("commit %s is not the latest commit on branch %s (latest: %s)", commitHash, branch, branchInfo.Commit.SHA)
	}

	// 2. Find open PR for this branch
	pr, err := c.findPRForBranch(owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("failed to find PR for branch: %w", err)
	}

	if pr == nil {
		return "", fmt.Errorf("no open pull request found for branch %s", branch)
	}

	return pr.HTMLURL, nil
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
