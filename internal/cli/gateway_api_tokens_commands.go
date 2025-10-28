package cli

import (
	"errors"
	"fmt"
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
			if token.CreatedByUserID != nil {
				if err := writef(cmd.OutOrStdout(), "Created By User ID: %d\n", *token.CreatedByUserID); err != nil {
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
