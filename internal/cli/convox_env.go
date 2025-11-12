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
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, _ []string) error {
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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
		// This is a custom gateway command to handle --unmask flag
		RunE: SilenceOnError(envGetGateway),
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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

	queryValues := url.Values{}
	if key != "" {
		queryValues.Set("key", key)
	}
	if unmask {
		queryValues.Set("secrets", "true")
	}
	envMap, err := fetchAppEnv(cmd, app, queryValues)
	if err != nil {
		return err
	}

	// Print value
	val, ok := envMap[key]
	if !ok {
		return fmt.Errorf("key %s not found", key)
	}
	fmt.Println(val)
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

	envMap, err := fetchAppEnv(cmd, app, nil)
	if err != nil {
		return err
	}

	// Print in key=value format
	for key, val := range envMap {
		fmt.Printf("%s=%s\n", key, val)
	}

	return nil
}

func fetchAppEnv(_ *cobra.Command, app string, query url.Values) (map[string]string, error) {
	rack, err := SelectedRack()
	if err != nil {
		return nil, err
	}

	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return nil, err
	}

	base := fmt.Sprintf("%s/api/v1/apps/%s/env", gatewayURL, url.PathEscape(app))
	apiURL := base
	if query != nil {
		if encoded := query.Encode(); encoded != "" {
			apiURL = fmt.Sprintf("%s?%s", base, encoded)
		}
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch env: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("failed to fetch env: %s", errResp.Error)
		}
		return nil, fmt.Errorf("failed to fetch env: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Env, nil
}
