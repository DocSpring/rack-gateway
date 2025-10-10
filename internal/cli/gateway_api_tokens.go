package cli

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

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/spf13/cobra"
)

type apiToken struct {
	ID              int64      `json:"id"`
	PublicID        string     `json:"public_id"`
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
	Token    string    `json:"token"`
	APIToken *apiToken `json:"api_token"`
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

func writeLine(out io.Writer, args ...any) error {
	_, err := fmt.Fprintln(out, args...)
	return err
}

func writef(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format, args...)
	return err
}

func APITokenCommand() *cobra.Command {
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
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := SelectedRack()
			if err != nil {
				return err
			}

			tokens, err := fetchAPITokens(cmd, rack)
			if err != nil {
				return err
			}

			if outputJSON {
				return printJSON(cmd, tokens)
			}

			if len(tokens) == 0 {
				return writeLine(cmd.OutOrStdout(), "No API tokens found")
			}

			if err := writef(cmd.OutOrStdout(), "%-36s %-25s %-25s %-19s %-19s\n", "UUID", "NAME", "OWNER", "CREATED", "LAST USED"); err != nil {
				return err
			}
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
				if err := writef(cmd.OutOrStdout(), "%-36s %-25s %-25s %-19s %-19s\n", t.PublicID, t.Name, owner, created, lastUsed); err != nil {
					return err
				}
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
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := SelectedRack()
			if err != nil {
				return err
			}

			token, err := fetchAPITokenByID(cmd, rack, args[0])
			if err != nil {
				return err
			}

			if outputJSON {
				return printJSON(cmd, token)
			}

			if err := writef(cmd.OutOrStdout(), "Public ID: %s\n", token.PublicID); err != nil {
				return err
			}
			if err := writef(cmd.OutOrStdout(), "Internal ID: %d\n", token.ID); err != nil {
				return err
			}
			if err := writef(cmd.OutOrStdout(), "Name: %s\n", token.Name); err != nil {
				return err
			}
			if err := writef(cmd.OutOrStdout(), "Owner User ID: %d\n", token.UserID); err != nil {
				return err
			}
			if token.CreatedByEmail != "" {
				if err := writef(cmd.OutOrStdout(), "Created By: %s (%s)\n", token.CreatedByName, token.CreatedByEmail); err != nil {
					return err
				}
			}
			if err := writef(cmd.OutOrStdout(), "Created At: %s\n", token.CreatedAt.Format(time.RFC3339)); err != nil {
				return err
			}
			if token.ExpiresAt != nil {
				if err := writef(cmd.OutOrStdout(), "Expires At: %s\n", token.ExpiresAt.Format(time.RFC3339)); err != nil {
					return err
				}
			} else {
				if err := writef(cmd.OutOrStdout(), "Expires At: never\n"); err != nil {
					return err
				}
			}
			if token.LastUsedAt != nil {
				if err := writef(cmd.OutOrStdout(), "Last Used: %s\n", token.LastUsedAt.Format(time.RFC3339)); err != nil {
					return err
				}
			}
			sort.Strings(token.Permissions)
			if err := writef(cmd.OutOrStdout(), "Permissions:%s\n", formatList(token.Permissions)); err != nil {
				return err
			}
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
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}

			rack, err := SelectedRack()
			if err != nil {
				return err
			}

			metadata, err := fetchTokenPermissionMetadata(cmd, rack)
			if err != nil {
				return err
			}

			// Require either --role or --permission to be specified
			if role == "" && len(permissions) == 0 {
				return errors.New("--role or --permission is required")
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

			var expires *time.Time
			if strings.TrimSpace(expiresAt) != "" {
				parsed, err := time.Parse(time.RFC3339, expiresAt)
				if err != nil {
					return fmt.Errorf("invalid --expires-at value (use RFC3339): %w", err)
				}
				expires = &parsed
			}

			resp, err := createAPIToken(cmd, rack, name, userEmail, permSet.list(), expires)
			if err != nil {
				return err
			}

			if resp == nil || resp.APIToken == nil {
				return errors.New("gateway returned an incomplete API token payload")
			}
			if strings.TrimSpace(resp.APIToken.PublicID) == "" {
				return errors.New("gateway returned API token without a public id")
			}
			if strings.TrimSpace(resp.APIToken.Name) == "" {
				return errors.New("gateway returned API token without a name")
			}

			switch strings.ToLower(output) {
			case "json":
				return printJSON(cmd, resp)
			case "token":
				return writeLine(cmd.OutOrStdout(), resp.Token)
			case "text", "", "table":
				if err := writef(cmd.OutOrStdout(), "Created token %q (id %s)\n", resp.APIToken.Name, resp.APIToken.PublicID); err != nil {
					return err
				}
				return writef(cmd.OutOrStdout(), "Token: %s\n", resp.Token)
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
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			rack, err := SelectedRack()
			if err != nil {
				return err
			}
			if err := deleteAPIToken(cmd, rack, args[0]); err != nil {
				return err
			}
			return writef(cmd.OutOrStdout(), "Deleted token %s\n", args[0])
		}),
	}
	return cmd
}

func fetchAPITokens(cmd *cobra.Command, rack string) ([]apiToken, error) {
	var tokens []apiToken
	if err := gatewayRequest(cmd, rack, http.MethodGet, "/admin/tokens", nil, &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func fetchAPITokenByID(cmd *cobra.Command, rack, id string) (*apiToken, error) {
	var token apiToken
	if err := gatewayRequest(cmd, rack, http.MethodGet, "/admin/tokens/"+url.PathEscape(id), nil, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func fetchTokenPermissionMetadata(cmd *cobra.Command, rack string) (*tokenPermissionMetadata, error) {
	var metadata tokenPermissionMetadata
	if err := gatewayRequest(cmd, rack, http.MethodGet, "/admin/tokens/permissions", nil, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func createAPIToken(cmd *cobra.Command, rack, name, userEmail string, permissions []string, expiresAt *time.Time) (*apiTokenResponse, error) {
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
	if err := gatewayRequest(cmd, rack, http.MethodPost, "/admin/tokens", payload, &resp); err != nil {
		return nil, err
	}
	if resp.APIToken == nil {
		return nil, fmt.Errorf("gateway returned missing api_token metadata")
	}
	return &resp, nil
}

func deleteAPIToken(cmd *cobra.Command, rack, id string) error {
	return gatewayRequest(cmd, rack, http.MethodDelete, "/admin/tokens/"+url.PathEscape(id), nil, nil)
}

// gatewayMFAContext holds MFA requirement info for a gateway request
type gatewayMFAContext struct {
	mfaLevel convox.MFALevel
	mfaAuth  string // inline MFA auth string (e.g., "totp.123456") for MFAAlways
}

// checkAndPromptGatewayMFA checks if the gateway operation requires MFA and returns MFA context
func checkAndPromptGatewayMFA(cmd *cobra.Command, baseURL, bearer, rack, method, path string) (*gatewayMFAContext, error) {
	// Map gateway API paths to permissions
	var permissions []string

	// API Token operations
	if strings.HasPrefix(path, "/admin/tokens") {
		switch method {
		case http.MethodPost:
			permissions = []string{"gateway:api_token:create"}
		case http.MethodPut, http.MethodPatch:
			permissions = []string{"gateway:api_token:update"}
		case http.MethodDelete:
			permissions = []string{"gateway:api_token:delete"}
		}
	}

	// Deploy Approval operations
	if strings.Contains(path, "/deploy-approval-requests") && strings.Contains(path, "/approve") {
		permissions = []string{"gateway:deploy_approval_request:approve"}
	}

	// If no permissions identified, no MFA required
	if len(permissions) == 0 {
		return &gatewayMFAContext{mfaLevel: convox.MFANone}, nil
	}

	mfaLevel := convox.GetMFALevel(permissions)
	if mfaLevel == convox.MFANone {
		return &gatewayMFAContext{mfaLevel: convox.MFANone}, nil
	}

	ctx := &gatewayMFAContext{mfaLevel: mfaLevel}

	// For MFAAlways: always prompt upfront and get inline auth string
	if mfaLevel == convox.MFAAlways {
		mfaAuth, err := promptMFAForCommand(cmd, baseURL, bearer, rack)
		if err != nil {
			return nil, err
		}
		ctx.mfaAuth = mfaAuth
		return ctx, nil
	}

	// For MFAStepUp: don't prompt upfront - let server tell us if needed
	// The retry logic will prompt and include inline MFA if server returns mfa_required
	return ctx, nil
}

func gatewayRequest(cmd *cobra.Command, rack, method, path string, body interface{}, out interface{}) error {
	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return err
	}

	// Check if this operation requires MFA and prompt BEFORE making the request
	mfaCtx, err := checkAndPromptGatewayMFA(cmd, gatewayURL, bearer, rack, method, path)
	if err != nil {
		return err
	}

	// Attach inline MFA to bearer token if needed
	requestBearer := bearer
	if mfaCtx.mfaAuth != "" {
		// Format: session_token.mfa_type.mfa_value (e.g., "session123.totp.123456")
		requestBearer = bearer + "." + mfaCtx.mfaAuth
	}

	// Try the request
	statusCode, responseBody, err := doGatewayRequest(gatewayURL, requestBearer, method, path, body)
	if err != nil {
		return err
	}

	// Smart retry: if server unexpectedly requires MFA, adapt and retry once
	if statusCode == http.StatusUnauthorized {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(responseBody, &errResp) == nil && errResp.Error == "mfa_required" {
			// Server requires MFA but we didn't provide it - this can happen when:
			// 1. CLI thinks step-up is still valid but server disagrees (clock skew/latency)
			// 2. Server upgraded endpoint to require MFA (version mismatch)
			// Solution: Prompt for MFA and retry with inline auth

			mfaAuth, err := promptMFAForCommand(cmd, gatewayURL, bearer, rack)
			if err != nil {
				return fmt.Errorf("MFA required by server: %w", err)
			}
			// Retry with inline MFA
			statusCode, responseBody, err = doGatewayRequest(gatewayURL, bearer+"."+mfaAuth, method, path, body)
			if err != nil {
				return err
			}
		}
	}

	// Check for errors after potential retry
	if statusCode >= 400 {
		return fmt.Errorf("gateway request failed (%d): %s", statusCode, strings.TrimSpace(string(responseBody)))
	}

	// Decode response if output pointer provided
	if out != nil {
		if err := json.Unmarshal(responseBody, out); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// doGatewayRequest performs the actual HTTP request and returns status, body, and error
func doGatewayRequest(gatewayURL, bearer, method, path string, body interface{}) (int, []byte, error) {
	fullURL := gatewayURL + "/.gateway/api" + path

	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, fullURL, payload)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // response cleanup

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}

	return resp.StatusCode, responseBody, nil
}

func gatewayAuthInfo(rack string) (string, string, error) {
	gatewayURL := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL"))
	if gatewayURL == "" {
		var err error
		gatewayURL, err = LoadGatewayURL(rack)
		if err != nil {
			return "", "", err
		}
	}

	normalized, err := NormalizeGatewayURL(gatewayURL)
	if err != nil {
		return "", "", err
	}

	bearer := strings.TrimSpace(APITokenFlag)
	if bearer == "" {
		bearer = strings.TrimSpace(os.Getenv("RACK_GATEWAY_API_TOKEN"))
	}
	if bearer == "" {
		token, err := LoadToken(rack)
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
	return writeLine(cmd.OutOrStdout(), string(data))
}
