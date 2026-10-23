package circleci

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// maxListPages bounds how many pages of a CircleCI listing are read. A workflow's jobs or a pipeline's workflows
// fill a few pages at most.
const maxListPages = 20

// listItems returns the items of every page of a CircleCI listing, following next_page_token.
func listItems[T any](c *Client, listURL string) ([]T, error) {
	var items []T
	pageURL := listURL
	for range maxListPages {
		body, err := c.doRequest(http.MethodGet, pageURL)
		if err != nil {
			return nil, err
		}
		var page struct {
			Items         []T    `json:"items"`
			NextPageToken string `json:"next_page_token"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse the listing: %w", err)
		}
		items = append(items, page.Items...)
		if page.NextPageToken == "" {
			return items, nil
		}
		pageURL = listURL + "?page-token=" + url.QueryEscape(page.NextPageToken)
	}
	return nil, fmt.Errorf("the listing has more than %d pages", maxListPages)
}
