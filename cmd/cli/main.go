package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type Config struct {
	Gateways map[string]GatewayConfig `json:"gateways"`
	Tokens   map[string]Token         `json:"tokens"`
}

type GatewayConfig struct {
	URL string `json:"url"`
}

type Token struct {
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

type LoginStartResponse struct {
	AuthURL      string `json:"auth_url"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

type LoginCallbackRequest struct {
	Code         string `json:"code"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

var (
	configPath string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "convox-gateway",
		Short: "Enterprise gateway for Convox with authentication and RBAC",
		Long: `Convox Gateway provides secure authenticated access to Convox racks
with SSO authentication, role-based access control, and audit logging.

This tool wraps the convox CLI. Any command not listed below will be
passed through to convox with proper authentication. For example:
  convox-gateway apps
  convox-gateway ps
  convox-gateway deploy

To see help for the underlying convox CLI, use:
  convox-gateway convox-help`,
		Run: func(cmd *cobra.Command, args []string) {
			// If no subcommand is specified, show help
			cmd.Help()
		},
	}

	loginCmd := &cobra.Command{
		Use:   "login [rack] [gateway-url]",
		Short: "Login to a Convox rack via OAuth",
		Long:  "Authenticate with SSO provider and store token for the specified rack.\n\nExample: convox-gateway login staging https://convox-gateway.company.com",
		Args:  cobra.ExactArgs(2),
		RunE:  loginCommand,
	}

	convoxHelpCmd := &cobra.Command{
		Use:   "convox-help",
		Short: "Show help for the convox CLI",
		Long:  "Display the help output from the actual convox CLI that this gateway wraps",
		Run: func(cmd *cobra.Command, args []string) {
			// Pass through to convox CLI help
			convoxCmd := exec.Command("convox", "help")
			convoxCmd.Stdout = os.Stdout
			convoxCmd.Stderr = os.Stderr
			convoxCmd.Run()
		},
	}

	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for convox-gateway.

To load completions:

Bash:
  $ source <(convox-gateway completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ convox-gateway completion bash > /etc/bash_completion.d/convox-gateway
  # macOS:
  $ convox-gateway completion bash > $(brew --prefix)/etc/bash_completion.d/convox-gateway

Zsh:
  $ source <(convox-gateway completion zsh)
  # To load completions for each session, execute once:
  $ convox-gateway completion zsh > "${fpath[1]}/_convox-gateway"

Fish:
  $ convox-gateway completion fish | source
  # To load completions for each session, execute once:
  $ convox-gateway completion fish > ~/.config/fish/completions/convox-gateway.fish

PowerShell:
  PS> convox-gateway completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> convox-gateway completion powershell > convox-gateway.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactValidArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}
		},
	}

	rootCmd.AddCommand(loginCmd, convoxHelpCmd, completionCmd)

	// Allow config path to be set via environment variable or flag
	defaultConfigPath := getEnv("CONVOX_GATEWAY_CONFIG", filepath.Join(homeDir(), ".config", "convox-gateway"))
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "Config directory")

	// Check if we should wrap a convox command
	if len(os.Args) > 1 && os.Args[1] != "login" && os.Args[1] != "completion" && os.Args[1] != "convox-help" && os.Args[1] != "help" && os.Args[1] != "--help" && os.Args[1] != "-h" {
		// This is a convox command to be wrapped
		if err := wrapConvoxCommand(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func loginCommand(cmd *cobra.Command, args []string) error {
	rack := args[0]
	gatewayURL := args[1]

	// Save gateway URL for this rack
	if err := saveGatewayConfig(rack, gatewayURL); err != nil {
		return fmt.Errorf("failed to save gateway config: %w", err)
	}

	fmt.Printf("Starting login for rack: %s via gateway: %s\n", rack, gatewayURL)

	startResp, err := startLogin(gatewayURL)
	if err != nil {
		return fmt.Errorf("failed to start login: %w", err)
	}

	fmt.Printf("Opening browser for authentication...\n")
	if err := openBrowser(startResp.AuthURL); err != nil {
		fmt.Printf("Please open this URL in your browser:\n%s\n", startResp.AuthURL)
	}

	fmt.Print("Enter the authorization code from the callback URL: ")
	var code string
	fmt.Scanln(&code)

	loginResp, err := completeLogin(gatewayURL, code, startResp.State, startResp.CodeVerifier)
	if err != nil {
		return fmt.Errorf("failed to complete login: %w", err)
	}

	if err := saveToken(rack, loginResp); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Printf("Successfully logged in as %s\n", loginResp.Email)
	fmt.Printf("Token expires at: %s\n", loginResp.ExpiresAt.Format(time.RFC3339))

	return nil
}

func wrapConvoxCommand(args []string) error {
	// Determine which rack to use (could be from flag, env var, or default)
	rack := "staging" // Default rack

	// Check for --rack flag
	for i, arg := range args {
		if arg == "--rack" && i+1 < len(args) {
			rack = args[i+1]
		}
	}

	// Check for rack from environment
	if envRack := os.Getenv("CONVOX_RACK"); envRack != "" {
		rack = envRack
	}

	// Load gateway config and token for the rack
	gatewayURL, err := loadGatewayURL(rack)
	if err != nil {
		return fmt.Errorf("rack %s not configured. Run: convox-gateway login %s <gateway-url>", rack, rack)
	}

	token, err := loadToken(rack)
	if err != nil {
		return fmt.Errorf("not logged in to rack %s. Run: convox-gateway login %s %s", rack, rack, gatewayURL)
	}

	// Check if token is expired
	if time.Now().After(token.ExpiresAt) {
		return fmt.Errorf("token expired for rack %s. Run: convox-gateway login %s %s", rack, rack, gatewayURL)
	}

	// Build RACK_URL with JWT token as password
	parsedURL := gatewayURL
	if !strings.HasPrefix(parsedURL, "http://") && !strings.HasPrefix(parsedURL, "https://") {
		parsedURL = "https://" + parsedURL
	}
	parsedURL = strings.TrimSuffix(parsedURL, "/")

	var rackURL string
	if strings.HasPrefix(parsedURL, "http://") {
		// For local testing, preserve http
		rackURL = fmt.Sprintf("http://convox:%s@%s/v1/proxy/%s",
			token.Token,
			strings.TrimPrefix(parsedURL, "http://"),
			rack)
	} else {
		rackURL = fmt.Sprintf("https://convox:%s@%s/v1/proxy/%s",
			token.Token,
			strings.TrimPrefix(parsedURL, "https://"),
			rack)
	}

	// Execute the convox CLI with RACK_URL set
	cmd := exec.Command("convox", args...)
	cmd.Env = append(os.Environ(), "RACK_URL="+rackURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func startLogin(gatewayURL string) (*LoginStartResponse, error) {
	parsedURL := gatewayURL
	if !strings.HasPrefix(parsedURL, "http://") && !strings.HasPrefix(parsedURL, "https://") {
		parsedURL = "https://" + parsedURL
	}
	url := fmt.Sprintf("%s/v1/login/start", strings.TrimSuffix(parsedURL, "/"))

	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login start failed: %s", string(body))
	}

	var result LoginStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func completeLogin(gatewayURL, code, state, codeVerifier string) (*LoginResponse, error) {
	parsedURL := gatewayURL
	if !strings.HasPrefix(parsedURL, "http://") && !strings.HasPrefix(parsedURL, "https://") {
		parsedURL = "https://" + parsedURL
	}
	url := fmt.Sprintf("%s/v1/login/callback", strings.TrimSuffix(parsedURL, "/"))

	payload := LoginCallbackRequest{
		Code:         code,
		State:        state,
		CodeVerifier: codeVerifier,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login callback failed: %s", string(body))
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func saveToken(rack string, loginResp *LoginResponse) error {
	if err := os.MkdirAll(configPath, 0700); err != nil {
		return err
	}

	configFile := filepath.Join(configPath, "config.json")

	config := &Config{
		Gateways: make(map[string]GatewayConfig),
		Tokens:   make(map[string]Token),
	}

	if data, err := os.ReadFile(configFile); err == nil {
		json.Unmarshal(data, config)
	}

	if config.Gateways == nil {
		config.Gateways = make(map[string]GatewayConfig)
	}
	if config.Tokens == nil {
		config.Tokens = make(map[string]Token)
	}

	config.Tokens[rack] = Token{
		Token:     loginResp.Token,
		Email:     loginResp.Email,
		ExpiresAt: loginResp.ExpiresAt,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0600)
}

func loadToken(rack string) (*Token, error) {
	configFile := filepath.Join(configPath, "config.json")

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	token, exists := config.Tokens[rack]
	if !exists {
		return nil, fmt.Errorf("no token found for rack: %s", rack)
	}

	if time.Now().After(token.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return &token, nil
}

func saveGatewayConfig(rack, gatewayURL string) error {
	if err := os.MkdirAll(configPath, 0700); err != nil {
		return err
	}

	configFile := filepath.Join(configPath, "config.json")

	config := &Config{
		Gateways: make(map[string]GatewayConfig),
		Tokens:   make(map[string]Token),
	}

	if data, err := os.ReadFile(configFile); err == nil {
		json.Unmarshal(data, config)
	}

	if config.Gateways == nil {
		config.Gateways = make(map[string]GatewayConfig)
	}
	if config.Tokens == nil {
		config.Tokens = make(map[string]Token)
	}

	config.Gateways[rack] = GatewayConfig{
		URL: gatewayURL,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0600)
}

func loadGatewayURL(rack string) (string, error) {
	configFile := filepath.Join(configPath, "config.json")

	data, err := os.ReadFile(configFile)
	if err != nil {
		return "", err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return "", err
	}

	gateway, exists := config.Gateways[rack]
	if !exists {
		return "", fmt.Errorf("no gateway configured for rack: %s", rack)
	}

	return gateway.URL, nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	return ""
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
