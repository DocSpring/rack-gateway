package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func CompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for rack-gateway.

To load completions:

Bash:
  $ source <(rack-gateway completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ rack-gateway completion bash > /etc/bash_completion.d/rack-gateway
  # macOS:
  $ rack-gateway completion bash > $(brew --prefix)/etc/bash_completion.d/rack-gateway

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ rack-gateway completion zsh > "${fpath[1]}/_rack-gateway"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ rack-gateway completion fish | source
  # To load completions for each session, execute once:
  $ rack-gateway completion fish > ~/.config/fish/completions/rack-gateway.fish

PowerShell:
  PS> rack-gateway completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> rack-gateway completion powershell > rack-gateway.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell type %q", args[0])
			}
		},
	}

	return cmd
}
