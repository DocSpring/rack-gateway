package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type apiToken struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	UserID          int64      `json:"user_id"`
	CreatedByUserID *int64     `json:"created_by_user_id"`
	CreatedByEmail  string     `json:"created_by_email,omitempty"`
	CreatedByName   string     `json:"created_by_name,omitempty"`
	Permissions     []string   `json:"permissions"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       *time.Time `json:"expires_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
}

type apiTokenResponse struct {
	Token    string   `json:"token"`
	APIToken apiToken `json:"api_token"`
}

type tokenPermissionMetadata struct {
	Permissions        []string     `json:"permissions"`
	Roles              []roleOption `json:"roles"`
	DefaultPermissions []string     `json:"default_permissions"`
}

type roleOption struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func newAPITokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-token",
		Short: "Manage API tokens for the current gateway",
	}

	cmd.AddCommand(newAPITokenListCommand())
	cmd.AddCommand(newAPITokenGetCommand())
	cmd.AddCommand(newAPITokenCreateCommand())
	cmd.AddCommand(newAPITokenDeleteCommand())

	return cmd
}

func newAPITokenListCommand() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List API tokens",
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := selectedRack()
			if err != nil {
				return err
			}

			tokens, err := fetchAPITokens(rack)
			if err != nil {
				return err
			}

			if outputJSON {
				return printJSON(cmd, tokens)
			}

			if len(tokens) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No API tokens found")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-5s %-25s %-25s %-19s %-19s\n", "ID", "NAME", "OWNER", "CREATED", "LAST USED")
			for _, t := range tokens {
				owner := t.CreatedByEmail
				if owner == "" {
					owner = fmt.Sprintf("user:%d", t.UserID)
				}
				created := t.CreatedAt.Format(time.RFC3339)
				lastUsed := "-"
				if t.LastUsedAt != nil {
					lastUsed = t.LastUsedAt.Format(time.RFC3339)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-5d %-25s %-25s %-19s %-19s\n", t.ID, t.Name, owner, created, lastUsed)
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	return cmd
}

func newAPITokenGetCommand() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "get <token-id>",
		Short: "Show details for an API token",
		Args:  cobra.ExactArgs(1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := selectedRack()
			if err != nil {
				return err
			}

			token, err := fetchAPITokenByID(rack, args[0])
			if err != nil {
				return err
			}

			if outputJSON {
				return printJSON(cmd, token)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "ID: %d\n", token.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\n", token.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Owner User ID: %d\n", token.UserID)
			if token.CreatedByEmail != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Created By: %s (%s)\n", token.CreatedByName, token.CreatedByEmail)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created At: %s\n", token.CreatedAt.Format(time.RFC3339))
			if token.ExpiresAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Expires At: %s\n", token.ExpiresAt.Format(time.RFC3339))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Expires At: never\n")
			}
			if token.LastUsedAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Last Used: %s\n", token.LastUsedAt.Format(time.RFC3339))
			}
			sort.Strings(token.Permissions)
			fmt.Fprintf(cmd.OutOrStdout(), "Permissions:%s\n", formatList(token.Permissions))
			return nil
		}),
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	return cmd
}

func newAPITokenCreateCommand() *cobra.Command {
	var name string
	var userEmail string
	var permissions []string
	var role string
	var expiresAt string
	var output string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API token",
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}

			rack, err := selectedRack()
			if err != nil {
				return err
			}

			metadata, err := fetchTokenPermissionMetadata(rack)
			if err != nil {
				return err
			}

			permSet := newPermissionSet()
			if role != "" {
				matched := false
				for _, option := range metadata.Roles {
					if option.Name == role {
						permSet.add(option.Permissions...)
						matched = true
						break
					}
				}
				if !matched {
					return fmt.Errorf("unknown role %q", role)
				}
			}
			permSet.add(permissions...)
			if permSet.len() == 0 {
				permSet.add(metadata.DefaultPermissions...)
			}

			var expires *time.Time
			if strings.TrimSpace(expiresAt) != "" {
				parsed, err := time.Parse(time.RFC3339, expiresAt)
				if err != nil {
					return fmt.Errorf("invalid --expires-at value (use RFC3339): %w", err)
				}
				expires = &parsed
			}

			resp, err := createAPIToken(rack, name, userEmail, permSet.list(), expires)
			if err != nil {
				return err
			}

			switch strings.ToLower(output) {
			case "json":
				return printJSON(cmd, resp)
			case "token":
				fmt.Fprintln(cmd.OutOrStdout(), resp.Token)
				return nil
			case "text", "", "table":
				fmt.Fprintf(cmd.OutOrStdout(), "Created token %q (id %d)\n", resp.APIToken.Name, resp.APIToken.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "Token: %s\n", resp.Token)
				return nil
			default:
				return fmt.Errorf("unknown --output value %q", output)
			}
		}),
	}

	cmd.Flags().StringVar(&name, "name", "", "Display name for the token")
	cmd.Flags().StringVar(&userEmail, "user", "", "Create token for another user (admin only)")
	cmd.Flags().StringSliceVar(&permissions, "permission", nil, "Permission to grant (repeatable)")
	cmd.Flags().StringVar(&role, "role", "", "Shortcut role to grant (viewer, ops, deployer, cicd, admin)")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "Optional expiration time (RFC3339)")
	cmd.Flags().StringVar(&output, "output", "text", "Output format: text, json, token")

	return cmd
}

func newAPITokenDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <token-id>",
		Short: "Delete an API token",
		Args:  cobra.ExactArgs(1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := selectedRack()
			if err != nil {
				return err
			}
			if err := deleteAPIToken(rack, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted token %s\n", args[0])
			return nil
		}),
	}
	return cmd
}

func fetchAPITokens(rack string) ([]apiToken, error) {
	var tokens []apiToken
	if err := gatewayRequest(rack, http.MethodGet, "/admin/tokens", nil, &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func fetchAPITokenByID(rack, id string) (*apiToken, error) {
	var token apiToken
	if err := gatewayRequest(rack, http.MethodGet, "/admin/tokens/"+url.PathEscape(id), nil, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func fetchTokenPermissionMetadata(rack string) (*tokenPermissionMetadata, error) {
	var metadata tokenPermissionMetadata
	if err := gatewayRequest(rack, http.MethodGet, "/admin/tokens/permissions", nil, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func createAPIToken(rack, name, userEmail string, permissions []string, expiresAt *time.Time) (*apiTokenResponse, error) {
	payload := map[string]interface{}{
		"name":        name,
		"permissions": permissions,
	}
	if userEmail != "" {
		payload["user_email"] = userEmail
	}
	if expiresAt != nil {
		payload["expires_at"] = expiresAt.Format(time.RFC3339)
	}

	var resp apiTokenResponse
	if err := gatewayRequest(rack, http.MethodPost, "/admin/tokens", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func deleteAPIToken(rack, id string) error {
	return gatewayRequest(rack, http.MethodDelete, "/admin/tokens/"+url.PathEscape(id), nil, nil)
}

func gatewayRequest(rack, method, path string, body interface{}, out interface{}) error {
	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return err
	}

	fullURL := gatewayURL + "/.gateway/api" + path

	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, fullURL, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	if requiresCSRF(method) {
		token, cookies, err := fetchCSRFToken(client, gatewayURL, bearer)
		if err != nil {
			return err
		}
		req.Header.Set("X-CSRF-Token", token)
		for _, c := range cookies {
			req.AddCookie(c)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return err
		}
	}
	return nil
}

func requiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead:
		return false
	default:
		return true
	}
}

func fetchCSRFToken(client *http.Client, gatewayURL, bearer string) (string, []*http.Cookie, error) {
	req, err := http.NewRequest(http.MethodGet, gatewayURL+"/.gateway/api/auth/web/csrf", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("csrf fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", nil, err
	}
	if payload.Token == "" {
		return "", nil, fmt.Errorf("csrf token missing in response")
	}

	return payload.Token, resp.Cookies(), nil
}

func gatewayAuthInfo(rack string) (string, string, error) {
	gatewayURL := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_URL"))
	if gatewayURL == "" {
		var err error
		gatewayURL, err = loadGatewayURL(rack)
		if err != nil {
			return "", "", err
		}
	}

	normalized, err := normalizeGatewayURL(gatewayURL)
	if err != nil {
		return "", "", err
	}

	bearer := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_API_TOKEN"))
	if bearer == "" {
		token, err := loadToken(rack)
		if err != nil {
			return "", "", err
		}
		if time.Now().After(token.ExpiresAt) {
			return "", "", fmt.Errorf("token expired")
		}
		bearer = token.Token
	}

	return normalized, bearer, nil
}

func formatList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	sort.Strings(values)
	return "\n  - " + strings.Join(values, "\n  - ")
}

type permissionSet struct {
	items map[string]struct{}
}

func newPermissionSet() *permissionSet {
	return &permissionSet{items: map[string]struct{}{}}
}

func (p *permissionSet) add(perms ...string) {
	for _, perm := range perms {
		perm = strings.TrimSpace(strings.ToLower(perm))
		if perm == "" {
			continue
		}
		p.items[perm] = struct{}{}
	}
}

func (p *permissionSet) len() int {
	return len(p.items)
}

func (p *permissionSet) list() []string {
	out := make([]string, 0, len(p.items))
	for perm := range p.items {
		out = append(out, perm)
	}
	sort.Strings(out)
	return out
}

func printJSON(cmd *cobra.Command, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
