package cli

import (
	"net/textproto"
	"strings"

	"github.com/convox/convox/pkg/cli"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

// PsCommand creates the ps command
func PsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "list app processes",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			if err := resolveAppFlag(cobraCmd); err != nil {
				return err
			}
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "ps")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Ps(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)
	cmd.Flags().String("release", "", "filter by release")
	cmd.Flags().StringP("service", "s", "", "filter by service")
	cmd.Flags().Bool("all", false, "show all processes")

	// Add subcommands
	cmd.AddCommand(psInfoCommand())
	cmd.AddCommand(psStopCommand())

	return cmd
}

func psInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <id>",
		Short: "get information about a process",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			if err := resolveAppFlag(cobraCmd); err != nil {
				return err
			}
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "ps info")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.PsInfo(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)

	return cmd
}

func psStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <id>",
		Short: "stop a process",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			if err := resolveAppFlag(cobraCmd); err != nil {
				return err
			}
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "ps stop")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.PsStop(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)

	return cmd
}

// ExecCommand creates the exec command
func ExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <pid> <command>",
		Short: "execute a command in a running process",
		Args:  cobra.MinimumNArgs(2),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			client, ctx, err := setupProcessCommand(cobraCmd, args, "exec")
			if err != nil {
				return err
			}
			return normalizeConvoxExit(cli.Exec(client, ctx))
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)
	cmd.Flags().String("entrypoint", "", "override entrypoint")
	addJustificationFlag(cmd)

	return cmd
}

// RunCommand creates the run command
func RunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [service] [command]",
		Short: "run a one-off process",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			client, ctx, err := setupProcessCommand(cobraCmd, args, "run")
			if err != nil {
				return err
			}
			return normalizeConvoxExit(cli.Run(client, ctx))
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)
	cmd.Flags().BoolP("detach", "d", false, "run in detached mode")
	cmd.Flags().String("entrypoint", "", "override entrypoint")
	cmd.Flags().String("release", "", "release to run")
	cmd.Flags().StringSlice("env", []string{}, "environment variables")
	addJustificationFlag(cmd)

	return cmd
}

// setupProcessCommand prepares exec and run, whose first argument is the process (exec) or service (run) and the
// rest the command to run (Convox joins them with spaces). The gateway refuses a command too long for its audit
// log, or without a justification in an app that requires one, but the Convox SDK drops the answer to a refused
// WebSocket handshake, so an exec it refused would only show "websocket: bad handshake". Checking the command here
// explains the refusal, before any MFA prompt or process start.
func setupProcessCommand(
	cobraCmd *cobra.Command, args []string, action string,
) (*processClient, *stdcli.Context, error) {
	if err := resolveAppFlag(cobraCmd); err != nil {
		return nil, nil, err
	}
	if err := audit.CheckCommandLength(sentCommand(args[1:])); err != nil {
		return nil, nil, err
	}
	app, err := cobraCmd.Flags().GetString("app")
	if err != nil {
		return nil, nil, err
	}
	justification, err := commandJustification(cobraCmd, app)
	if err != nil {
		return nil, nil, err
	}
	rack, err := SelectedRack()
	if err != nil {
		return nil, nil, err
	}
	headers := map[string]string{}
	if justification != "" {
		headers[audit.JustificationHeader] = audit.EncodeJustification(justification)
	}
	prompt := commandPrompt(action, app, rack, args[0], args[1:], justification)
	client, err := newProcessClient(cobraCmd, headers, prompt)
	if err != nil {
		return nil, nil, err
	}
	return client, newConvoxContext(cobraCmd, args), nil
}

// headerLineBreaks are what Go's HTTP client replaces with spaces in a header value.
var headerLineBreaks = strings.NewReplacer("\n", " ", "\r", " ")

// sentCommand is the command words as the gateway receives them in the Command header: joined with spaces, line
// breaks made spaces and the ends trimmed, as Go's HTTP client writes a header value.
func sentCommand(words []string) string {
	return textproto.TrimString(headerLineBreaks.Replace(strings.Join(words, " ")))
}

// RestartCommand creates the restart command
func RestartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "restart an app",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			if err := resolveAppFlag(cobraCmd); err != nil {
				return err
			}
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "restart")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Restart(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)

	return cmd
}

// ScaleCommand creates the scale command
func ScaleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scale <service>",
		Short: "scale app processes",
		Args:  validateScaleArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			if err := resolveAppFlag(cobraCmd); err != nil {
				return err
			}
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "scale")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Scale(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)
	cmd.Flags().Int("count", 0, "number of processes")
	cmd.Flags().Int("cpu", 0, "cpu allocation")
	cmd.Flags().Int("memory", 0, "memory allocation")

	return cmd
}

func validateScaleArgs(cmd *cobra.Command, args []string) error {
	if scaleUpdateFlagsChanged(cmd) {
		return cobra.ExactArgs(1)(cmd, args)
	}

	return cobra.NoArgs(cmd, args)
}

func scaleUpdateFlagsChanged(cmd *cobra.Command) bool {
	return flagChanged(cmd, "count") || flagChanged(cmd, "cpu") || flagChanged(cmd, "memory")
}

func flagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}
