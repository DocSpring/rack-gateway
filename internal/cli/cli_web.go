package cli

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// WebCommand returns the cobra command for opening the gateway web UI in a browser.
func WebCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "web",
		Short: "Open the gateway web UI in your browser",
		RunE:  runWebCommand,
	}
}

func runWebCommand(_ *cobra.Command, _ []string) error {
	currentRack, err := SelectedRack()
	if err != nil {
		return err
	}

	cfg, _, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	webURL, err := configuredWebURL(cfg, currentRack)
	if err != nil {
		return err
	}

	allowedHosts := configuredGatewayHosts(cfg)
	fmt.Printf("Opening %s in your browser...\n", webURL)

	if err := launchBrowser(webURL, allowedHosts); err != nil {
		fmt.Printf("Failed to open browser automatically: %v\n", err)
		fmt.Printf("Please open this URL manually: %s\n", webURL)
	}

	return nil
}

func configuredWebURL(cfg *Config, rack string) (string, error) {
	gatewayCfg, ok := cfg.Gateways[rack]
	if !ok {
		return "", fmt.Errorf("rack %q not found in config", rack)
	}
	if gatewayCfg.URL == "" {
		return "", fmt.Errorf("gateway URL not configured for rack %q", rack)
	}

	return gatewayCfg.URL + "/app", nil
}

func configuredGatewayHosts(cfg *Config) []string {
	allowedHosts := make([]string, 0, len(cfg.Gateways))
	for _, gateway := range cfg.Gateways {
		gatewayURL, err := url.Parse(gateway.URL)
		if err == nil && gatewayURL.Host != "" {
			allowedHosts = append(allowedHosts, gatewayURL.Host)
		}
	}

	return allowedHosts
}

func launchBrowser(urlStr string, allowedHosts []string) error {
	if len(allowedHosts) == 0 {
		return fmt.Errorf("no configured gateway hosts available for browser launch validation")
	}

	// Validate URL before launching browser
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid URL scheme: %s (must be http or https)", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL must include host")
	}

	// Verify URL is from a configured gateway (prevents launching arbitrary URLs)
	isConfiguredGateway := false
	for _, host := range allowedHosts {
		if host == parsedURL.Host {
			isConfiguredGateway = true
			break
		}
	}
	if !isConfiguredGateway {
		return fmt.Errorf("URL host must match a configured gateway")
	}

	// Reconstruct validated URL to ensure it's safe
	safeURL := parsedURL.String()

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", safeURL)
	case "linux":
		cmd = exec.Command("xdg-open", safeURL)
	case "windows":
		cmd = exec.Command("explorer", safeURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
