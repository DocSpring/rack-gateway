package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
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

func fetchAPITokens(cmd *cobra.Command, rack string) ([]apiToken, error) {
	var tokens []apiToken
	if err := gatewayRequest(cmd, rack, http.MethodGet, "/api-tokens", nil, &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func fetchAPITokenByID(cmd *cobra.Command, rack, id string) (*apiToken, error) {
	var token apiToken
	if err := gatewayRequest(cmd, rack, http.MethodGet, "/api-tokens/"+url.PathEscape(id), nil, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func fetchTokenPermissionMetadata(cmd *cobra.Command, rack string) (*tokenPermissionMetadata, error) {
	var metadata tokenPermissionMetadata
	if err := gatewayRequest(cmd, rack, http.MethodGet, "/api-tokens/permissions", nil, &metadata); err != nil {
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
	if err := gatewayRequest(cmd, rack, http.MethodPost, "/api-tokens", payload, &resp); err != nil {
		return nil, err
	}
	if resp.APIToken == nil {
		return nil, fmt.Errorf("gateway returned missing api_token metadata")
	}
	return &resp, nil
}

func deleteAPIToken(cmd *cobra.Command, rack, id string) error {
	return gatewayRequest(cmd, rack, http.MethodDelete, "/api-tokens/"+url.PathEscape(id), nil, nil)
}

// gatewayMFAContext holds MFA requirement info for a gateway request
type gatewayMFAContext struct {
	mfaLevel rbac.MFALevel
	mfaAuth  string // inline MFA auth string (e.g., "totp.123456") for MFAAlways
}

// checkAndPromptGatewayMFA checks if the gateway operation requires MFA and returns MFA context
func checkAndPromptGatewayMFA(cmd *cobra.Command, baseURL, bearer, rack, method, path string) (*gatewayMFAContext, error) {
	// Map gateway API paths to permissions
	var permissions []string

	// API Token operations
	if strings.HasPrefix(path, "/api-tokens") {
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
		return &gatewayMFAContext{mfaLevel: rbac.MFANone}, nil
	}

	mfaLevel := rbac.GetMFALevel(permissions)
	if mfaLevel == rbac.MFANone {
		return &gatewayMFAContext{mfaLevel: rbac.MFANone}, nil
	}

	ctx := &gatewayMFAContext{mfaLevel: mfaLevel}

	// For MFAAlways: always prompt upfront and get inline auth string
	if mfaLevel == rbac.MFAAlways {
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

	requestBearer := applyInlineMFA(bearer, mfaCtx)

	statusCode, responseBody, err := doGatewayRequest(gatewayURL, requestBearer, method, path, body)
	if err != nil {
		return err
	}

	statusCode, responseBody, err = maybeRetryWithMFA(cmd, statusCode, responseBody, gatewayURL, bearer, rack, method, path, body)
	if err != nil {
		return err
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

func applyInlineMFA(bearer string, ctx *gatewayMFAContext) string {
	if ctx == nil || ctx.mfaAuth == "" {
		return bearer
	}
	return bearer + "." + ctx.mfaAuth
}

func maybeRetryWithMFA(cmd *cobra.Command, statusCode int, responseBody []byte, gatewayURL, bearer, rack, method, path string, body interface{}) (int, []byte, error) {
	if statusCode != http.StatusUnauthorized {
		return statusCode, responseBody, nil
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(responseBody, &errResp) != nil || errResp.Error != "mfa_required" {
		return statusCode, responseBody, nil
	}

	mfaAuth, err := promptMFAForCommand(cmd, gatewayURL, bearer, rack)
	if err != nil {
		return 0, nil, fmt.Errorf("MFA required by server: %w", err)
	}

	return doGatewayRequest(gatewayURL, bearer+"."+mfaAuth, method, path, body)
}

// doGatewayRequest performs the actual HTTP request and returns status, body, and error
func doGatewayRequest(gatewayURL, bearer, method, path string, body interface{}) (int, []byte, error) {
	fullURL := gatewayURL + "/api/v1" + path

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
	normalized, defaultToken, err := LoadRackAuth(rack)
	if err != nil {
		return "", "", err
	}

	bearer := strings.TrimSpace(APITokenFlag)
	if bearer == "" {
		bearer = strings.TrimSpace(os.Getenv("RACK_GATEWAY_API_TOKEN"))
	}
	if bearer == "" {
		bearer = defaultToken
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
