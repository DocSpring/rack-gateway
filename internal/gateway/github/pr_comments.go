package github

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// commentsPerPage is the most comments GitHub returns in one page.
	commentsPerPage = 100
	// maxCommentPages bounds how many pages of a pull request's comments PRCommentExists reads.
	maxCommentPages = 10
)

// PRCommentExists reports whether a comment on the pull request contains marker.
func (c *Client) PRCommentExists(owner, repo string, prNumber int, marker string) (bool, error) {
	for page := 1; page <= maxCommentPages; page++ {
		query := url.Values{"per_page": {strconv.Itoa(commentsPerPage)}, "page": {strconv.Itoa(page)}}
		apiURL := c.repoURL(owner, repo, "issues", strconv.Itoa(prNumber), "comments") + "?" + query.Encode()
		var comments []struct {
			Body string `json:"body"`
		}
		if err := c.doRequest("GET", apiURL, nil, http.StatusOK, "", &comments); err != nil {
			return false, err
		}
		for _, comment := range comments {
			if strings.Contains(comment.Body, marker) {
				return true, nil
			}
		}
		if len(comments) < commentsPerPage {
			return false, nil
		}
	}
	return false, fmt.Errorf("the pull request has more than %d comments", maxCommentPages*commentsPerPage)
}
