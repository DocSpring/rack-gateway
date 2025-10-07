package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	slackAPIBase = "https://slack.com/api"
)

type Client struct {
	botToken   string
	httpClient *http.Client
}

func NewClient(botToken string) *Client {
	return &Client{
		botToken: botToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Channel represents a Slack channel
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsChannel  bool   `json:"is_channel"`
	IsGroup    bool   `json:"is_group"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
}

// ListChannelsResponse is the response from conversations.list
type ListChannelsResponse struct {
	OK               bool      `json:"ok"`
	Channels         []Channel `json:"channels"`
	Error            string    `json:"error,omitempty"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// PostMessageResponse is the response from chat.postMessage
type PostMessageResponse struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel,omitempty"`
	TS      string `json:"ts,omitempty"`
	Error   string `json:"error,omitempty"`
}

// OAuthV2Response is the response from oauth.v2.access
type OAuthV2Response struct {
	OK          bool   `json:"ok"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	BotUserID   string `json:"bot_user_id"`
	Team        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
	Error string `json:"error,omitempty"`
}

// ListChannels lists all channels the bot has access to
func (c *Client) ListChannels() ([]Channel, error) {
	var allChannels []Channel
	cursor := ""

	for {
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/conversations.list", slackAPIBase), nil)
		if err != nil {
			return nil, err
		}

		q := req.URL.Query()
		// Only request public channels - bot needs to be invited to private channels
		q.Add("types", "public_channel")
		q.Add("exclude_archived", "true")
		q.Add("limit", "200")
		if cursor != "" {
			q.Add("cursor", cursor)
		}
		req.URL.RawQuery = q.Encode()

		req.Header.Set("Authorization", "Bearer "+c.botToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close() //nolint:errcheck
			return nil, fmt.Errorf("slack API returned %d: %s", resp.StatusCode, string(body))
		}

		var result ListChannelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close() //nolint:errcheck
			return nil, err
		}
		resp.Body.Close() //nolint:errcheck

		if !result.OK {
			return nil, fmt.Errorf("slack API error: %s", result.Error)
		}

		allChannels = append(allChannels, result.Channels...)

		// Check if there are more pages
		if result.ResponseMetadata.NextCursor == "" {
			break
		}
		cursor = result.ResponseMetadata.NextCursor
	}

	return allChannels, nil
}

// PostMessage sends a message to a Slack channel
func (c *Client) PostMessage(channelID, text string, blocks []map[string]interface{}) error {
	payload := map[string]interface{}{
		"channel": channelID,
		"text":    text,
	}

	if len(blocks) > 0 {
		payload["blocks"] = blocks
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/chat.postMessage", slackAPIBase), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Read body for better error logging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var result PostMessageResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return fmt.Errorf("failed to decode response (status %d, body: %s): %w", resp.StatusCode, string(bodyBytes), err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s (response: %s)", result.Error, string(bodyBytes))
	}

	return nil
}

// ExchangeOAuthCode exchanges an OAuth code for an access token
func ExchangeOAuthCode(clientID, clientSecret, code, redirectURI string) (*OAuthV2Response, error) {
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/oauth.v2.access", slackAPIBase), bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result OAuthV2Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("slack OAuth error: %s", result.Error)
	}

	return &result, nil
}
