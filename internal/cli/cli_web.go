package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// WebCommand returns the cobra command for opening the gateway web UI in a browser.
func WebCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "web",
		Short: "Open the gateway web UI in your browser",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Resolve which rack to use (respects --rack flag)
			currentRack, err := SelectedRack()
			if err != nil {
				return err
			}

			cfg, _, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			gatewayCfg, ok := cfg.Gateways[currentRack]
			if !ok {
				return fmt.Errorf("rack %q not found in config", currentRack)
			}

			gatewayURL := gatewayCfg.URL
			if gatewayURL == "" {
				return fmt.Errorf("gateway URL not configured for rack %q", currentRack)
			}

			// Open the web UI URL
			webURL := gatewayURL + "/app"

			fmt.Printf("Opening %s in your browser...\n", webURL)

			if err := launchBrowser(webURL); err != nil {
				fmt.Printf("Failed to open browser automatically: %v\n", err)
				fmt.Printf("Please open this URL manually: %s\n", webURL)
				return nil
			}

			return nil
		},
	}
}

func launchBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
