package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// EnvCommand creates the env command
func EnvCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "manage environment variables",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			// This is a custom gateway command to handle secret masking
			return envListGateway(cobraCmd)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	// Add subcommands
	cmd.AddCommand(envEditCommand())
	cmd.AddCommand(envGetCommand())
	cmd.AddCommand(envSetCommand())
	cmd.AddCommand(envUnsetCommand())

	return cmd
}

func envEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "edit environment interactively",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env edit")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "promote", "replace")
			if err != nil {
				return err
			}
			return cli.EnvEdit(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().Bool("promote", false, "promote release after setting")
	cmd.Flags().StringSlice("replace", []string{}, "replace environment variables")

	return cmd
}

func envGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "get an environment variable",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			// This is a custom gateway command to handle --unmask flag
			return envGetGateway(cobraCmd, args)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().Bool("unmask", false, "show unmasked secret values (requires secret:read permission)")

	return cmd
}

func envSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key=value>...",
		Short: "set environment variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env set")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "id", "promote", "replace")
			if err != nil {
				return err
			}
			return cli.EnvSet(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("id", "", "release id")
	cmd.Flags().Bool("promote", false, "promote release after setting")
	cmd.Flags().StringSlice("replace", []string{}, "replace environment variables")

	return cmd
}

func envUnsetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <key>...",
		Short: "unset environment variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env unset")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "id", "promote")
			if err != nil {
				return err
			}
			return cli.EnvUnset(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("id", "", "release id")
	cmd.Flags().Bool("promote", false, "promote release after unsetting")

	return cmd
}

// envGetGateway handles env get through the gateway API (custom implementation)
func envGetGateway(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Get app name
	app, err := cmd.Flags().GetString("app")
	if err != nil {
		return err
	}
	if app == "" {
		app, err = ResolveApp("")
		if err != nil {
			return err
		}
	}

	// Get unmask flag (show unmasked secrets)
	unmask, _ := cmd.Flags().GetBool("unmask")

	// Get gateway URL and token
	rack, err := SelectedRack()
	if err != nil {
		return err
	}
	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return err
	}

	// Build API URL
	apiURL := fmt.Sprintf("%s/api/v1/env?app=%s&key=%s", gatewayURL, url.QueryEscape(app), url.QueryEscape(key))
	if unmask {
		apiURL += "&secrets=true"
	}

	// Create request
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch env: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("failed to fetch env: %s", errResp.Error)
		}
		return fmt.Errorf("failed to fetch env: HTTP %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Print value
	if val, ok := result.Env[key]; ok {
		fmt.Println(val)
	} else {
		return fmt.Errorf("key %s not found", key)
	}

	return nil
}

// envListGateway handles env list through the gateway API (custom implementation)
func envListGateway(cmd *cobra.Command) error {
	// Get app name
	app, err := cmd.Flags().GetString("app")
	if err != nil {
		return err
	}
	if app == "" {
		app, err = ResolveApp("")
		if err != nil {
			return err
		}
	}

	// Get gateway URL and token
	rack, err := SelectedRack()
	if err != nil {
		return err
	}
	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return err
	}

	// Build API URL (without --unmask flag, secrets are masked)
	apiURL := fmt.Sprintf("%s/api/v1/env?app=%s", gatewayURL, url.QueryEscape(app))

	// Create request
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch env: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("failed to fetch env: %s", errResp.Error)
		}
		return fmt.Errorf("failed to fetch env: HTTP %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Print in key=value format
	for key, val := range result.Env {
		fmt.Printf("%s=%s\n", key, val)
	}

	return nil
}
