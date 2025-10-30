package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newAPITokenListCommand() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List API tokens",
		RunE: SilenceOnError(func(cmd *cobra.Command, _ []string) error {
			return runAPITokenList(cmd, outputJSON)
		}),
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	return cmd
}

func runAPITokenList(cmd *cobra.Command, outputJSON bool) error {
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
	return renderTokenTable(cmd, tokens)
}

func renderTokenTable(cmd *cobra.Command, tokens []apiToken) error {
	headerFmt := "%-36s %-25s %-25s %-19s %-19s\n"
	if err := writef(cmd.OutOrStdout(), headerFmt, "UUID", "NAME", "OWNER", "CREATED", "LAST USED"); err != nil {
		return err
	}
	rowFmt := "%-36s %-25s %-25s %-19s %-19s\n"
	for _, token := range tokens {
		owner := token.CreatedByEmail
		if owner == "" {
			owner = fmt.Sprintf("user:%d", token.UserID)
		}
		lastUsed := "-"
		if token.LastUsedAt != nil {
			lastUsed = token.LastUsedAt.Format(time.RFC3339)
		}
		created := token.CreatedAt.Format(time.RFC3339)
		if err := writef(cmd.OutOrStdout(), rowFmt, token.PublicID, token.Name, owner, created, lastUsed); err != nil {
			return err
		}
	}
	return nil
}

func newAPITokenGetCommand() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "get <token-id>",
		Short: "Show details for an API token",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			return runAPITokenGet(cmd, args[0], outputJSON)
		}),
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	return cmd
}

func runAPITokenGet(cmd *cobra.Command, tokenID string, outputJSON bool) error {
	rack, err := SelectedRack()
	if err != nil {
		return err
	}

	token, err := fetchAPITokenByID(cmd, rack, tokenID)
	if err != nil {
		return err
	}

	if outputJSON {
		return printJSON(cmd, token)
	}
	return writeTokenDetails(cmd, token)
}

func writeTokenDetails(cmd *cobra.Command, token *apiToken) error {
	out := cmd.OutOrStdout()
	if err := writef(out, "Public ID: %s\n", token.PublicID); err != nil {
		return err
	}
	if err := writef(out, "Internal ID: %d\n", token.ID); err != nil {
		return err
	}
	if err := writef(out, "Name: %s\n", token.Name); err != nil {
		return err
	}
	if err := writef(out, "Owner User ID: %d\n", token.UserID); err != nil {
		return err
	}
	if token.CreatedByEmail != "" {
		if err := writef(out, "Created By: %s (%s)\n", token.CreatedByName, token.CreatedByEmail); err != nil {
			return err
		}
	}
	if token.CreatedByUserID != nil {
		if err := writef(out, "Created By User ID: %d\n", *token.CreatedByUserID); err != nil {
			return err
		}
	}
	if err := writef(out, "Created At: %s\n", token.CreatedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	if err := writeExpiry(out, token.ExpiresAt); err != nil {
		return err
	}
	if token.LastUsedAt != nil {
		if err := writef(out, "Last Used: %s\n", token.LastUsedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	sort.Strings(token.Permissions)
	return writef(out, "Permissions:%s\n", formatList(token.Permissions))
}

func writeExpiry(out io.Writer, expiresAt *time.Time) error {
	if expiresAt == nil {
		return writef(out, "Expires At: never\n")
	}
	return writef(out, "Expires At: %s\n", expiresAt.Format(time.RFC3339))
}

func newAPITokenCreateCommand() *cobra.Command {
	opts := createAPITokenOptions{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API token",
		RunE: SilenceOnError(func(cmd *cobra.Command, _ []string) error {
			return runAPITokenCreate(cmd, opts)
		}),
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "Display name for the token")
	cmd.Flags().StringVar(&opts.userEmail, "user", "", "Create token for another user (admin only)")
	cmd.Flags().StringSliceVar(&opts.permissions, "permission", nil, "Permission to grant (repeatable)")
	cmd.Flags().StringVar(&opts.role, "role", "", "Shortcut role to grant (viewer, ops, deployer, cicd, admin)")
	cmd.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Optional expiration time (RFC3339)")
	cmd.Flags().StringVar(&opts.output, "output", "text", "Output format: text, json, token")

	return cmd
}

type createAPITokenOptions struct {
	name        string
	userEmail   string
	permissions []string
	role        string
	expiresAt   string
	output      string
}

type createAPITokenConfig struct {
	rack        string
	name        string
	userEmail   string
	permissions []string
	expiresAt   *time.Time
	output      string
}

func runAPITokenCreate(cmd *cobra.Command, opts createAPITokenOptions) error {
	if strings.TrimSpace(opts.name) == "" {
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

	permissions, err := resolveTokenPermissions(metadata, opts.role, opts.permissions)
	if err != nil {
		return err
	}

	expiresAt, err := parseTokenExpiry(opts.expiresAt)
	if err != nil {
		return err
	}

	cfg := createAPITokenConfig{
		rack:        rack,
		name:        opts.name,
		userEmail:   opts.userEmail,
		permissions: permissions,
		expiresAt:   expiresAt,
		output:      opts.output,
	}

	return executeAPITokenCreate(cmd, cfg)
}

func resolveTokenPermissions(metadata *tokenPermissionMetadata, role string, manual []string) ([]string, error) {
	if strings.TrimSpace(role) == "" && len(manual) == 0 {
		return nil, errors.New("--role or --permission is required")
	}

	perms := newPermissionSet()
	if strings.TrimSpace(role) != "" {
		if err := addRolePermissions(perms, metadata.Roles, role); err != nil {
			return nil, err
		}
	}
	perms.add(manual...)
	return perms.list(), nil
}

func addRolePermissions(perms *permissionSet, roles []roleOption, name string) error {
	for _, option := range roles {
		if option.Name == name {
			perms.add(option.Permissions...)
			return nil
		}
	}
	return fmt.Errorf("unknown role %q", name)
}

func parseTokenExpiry(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("invalid --expires-at value (use RFC3339): %w", err)
	}
	return &parsed, nil
}

func executeAPITokenCreate(cmd *cobra.Command, cfg createAPITokenConfig) error {
	resp, err := createAPIToken(cmd, cfg.rack, cfg.name, cfg.userEmail, cfg.permissions, cfg.expiresAt)
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

	switch strings.ToLower(strings.TrimSpace(cfg.output)) {
	case "json":
		return printJSON(cmd, resp)
	case "token":
		return writeLine(cmd.OutOrStdout(), resp.Token)
	case "", "text", "table":
		out := cmd.OutOrStdout()
		if err := writef(out, "Created token %q (id %s)\n", resp.APIToken.Name, resp.APIToken.PublicID); err != nil {
			return err
		}
		return writef(out, "Token: %s\n", resp.Token)
	default:
		return fmt.Errorf("unknown --output value %q", cfg.output)
	}
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
