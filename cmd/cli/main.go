package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type rackStatus struct {
	Rack        string
	GatewayURL  string
	StatusLines []string
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
	rackFlag   string
	Version    = "dev"
	BuildTime  = "unknown"
)

func silenceOnError(fn func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		}
		return err
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:           "convox-gateway",
		Short:         "API gateway for Convox with authentication and RBAC",
		SilenceErrors: true,
		Long: `Convox Gateway provides secure authenticated access to Convox racks
with SSO authentication, role-based access control, and audit logging.

To run convox commands through the gateway:
  convox-gateway convox apps
  convox-gateway convox ps
  convox-gateway convox deploy

Recommended aliases for your shell:
  alias cx="convox-gateway convox"   # cx apps, cx ps, cx deploy
  alias cg="convox-gateway"          # cg login, cg switch, cg rack

Rack management:
  convox-gateway rack                # Show current rack
  convox-gateway racks               # List all racks
  convox-gateway switch <rack>       # Switch to a different rack
  convox-gateway login <rack> <url>  # Login to a new rack`,
		Run: func(cmd *cobra.Command, args []string) {
			// If no subcommand is specified, show help
			cmd.Help()
		},
	}

	var noOpen bool
	var authFile string
	loginCmd := &cobra.Command{
		Use:   "login [rack] [gateway-url]",
		Short: "Login to a Convox rack via OAuth",
		Long:  "Authenticate with SSO provider and store token for the specified rack.\n\nExample: convox-gateway login staging https://convox-gateway.example.com",
		Args:  cobra.ExactArgs(2),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			return loginCommandWithFlags(args, noOpen, authFile)
		}),
	}
	loginCmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open a browser; print auth URL and wait for completion")
	loginCmd.Flags().StringVar(&authFile, "auth-file", "", "Write AUTH_URL, STATE, CODE_VERIFIER to this file for automation")

	convoxCmd := &cobra.Command{
		Use:                "convox [command]",
		Short:              "Run a convox CLI command through the gateway",
		Long:               "Execute any convox CLI command with gateway authentication and the selected rack",
		DisableFlagParsing: true,
		SilenceUsage:       true, // Don't show usage on error
		SilenceErrors:      true, // We'll handle error printing ourselves
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				// If just "convox-gateway convox" is run, show convox help
				convoxCmd := exec.Command("convox", "help")
				convoxCmd.Stdout = os.Stdout
				convoxCmd.Stderr = os.Stderr
				return convoxCmd.Run()
			}
			err := wrapConvoxCommand(args)
			if err != nil {
				// Just print the error message, not the usage
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return nil
		},
	}

	// Env commands (match Convox: env lists; get gets; set delegates)
	var appFlag string
	var showSecrets bool
	envCmd := &cobra.Command{
		Use:   "env",
		Short: "List environment variables for an app (masked by default)",
		Args:  cobra.NoArgs, // prevent unknown subcommands like `env unknown`
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			app, err := resolveApp(appFlag)
			if err != nil {
				return err
			}
			rack, err := getCurrentRack()
			if err != nil || rack == "" {
				return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
			}
			gatewayURL, err := loadGatewayURL(rack)
			if err != nil {
				return err
			}
			tok, err := loadToken(rack)
			if err != nil {
				return err
			}
			m, err := fetchEnvViaAPI(gatewayURL, tok.Token, app, "", showSecrets)
			if err != nil {
				return err
			}
			for k, v := range m {
				fmt.Printf("%s=%s\n", k, v)
			}
			return nil
		}),
	}
	envCmd.Flags().StringVarP(&appFlag, "app", "a", "", "App name")
	envCmd.Flags().BoolVar(&showSecrets, "secrets", false, "Show secret values (requires permission)")

	envGetCmd := &cobra.Command{
		Use:   "get <KEY>",
		Short: "Get a single environment variable (masked by default)",
		Args:  cobra.ExactArgs(1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			key := args[0]
			app, err := resolveApp(appFlag)
			if err != nil {
				return err
			}
			rack, err := getCurrentRack()
			if err != nil || rack == "" {
				return fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
			}
			gatewayURL, err := loadGatewayURL(rack)
			if err != nil {
				return err
			}
			tok, err := loadToken(rack)
			if err != nil {
				return err
			}
			m, err := fetchEnvViaAPI(gatewayURL, tok.Token, app, key, showSecrets)
			if err != nil {
				return err
			}
			fmt.Println(m[key])
			return nil
		}),
	}
	envGetCmd.Flags().StringVarP(&appFlag, "app", "a", "", "App name")
	envGetCmd.Flags().BoolVar(&showSecrets, "secrets", false, "Show secret values (requires permission)")

	envSetAlias := &cobra.Command{
		Use:                "set",
		Short:              "Alias for 'convox env set' (delegates to Convox CLI)",
		DisableFlagParsing: true,
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			full := append([]string{"env", "set"}, args...)
			return wrapConvoxCommand(full)
		}),
	}

	envUnsetAlias := &cobra.Command{
		Use:                "unset",
		Short:              "Alias for 'convox env unset' (delegates to Convox CLI)",
		DisableFlagParsing: true,
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			full := append([]string{"env", "unset"}, args...)
			return wrapConvoxCommand(full)
		}),
	}

	envCmd.AddCommand(envGetCmd, envSetAlias, envUnsetAlias)

	rackCmd := &cobra.Command{
		Use:   "rack",
		Short: "Show current rack and gateway information",
		Args:  cobra.NoArgs,
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			status, err := resolveRackStatus(time.Now())
			if err != nil {
				return err
			}

			fmt.Printf("Current rack: %s\n", status.Rack)
			fmt.Printf("Gateway URL: %s\n", status.GatewayURL)
			for _, line := range status.StatusLines {
				fmt.Println(line)
			}

			return nil
		}),
	}

	racksCmd := &cobra.Command{
		Use:   "racks",
		Short: "List all configured racks",
		Args:  cobra.NoArgs,
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			configFile := filepath.Join(configPath, "config.json")
			data, err := os.ReadFile(configFile)
			if err != nil {
				return fmt.Errorf("no configuration found. Run: convox-gateway login <rack> <gateway-url>")
			}

			var config Config
			if err := json.Unmarshal(data, &config); err != nil {
				return err
			}

			currentRack, _ := getCurrentRack()

			if len(config.Gateways) == 0 {
				fmt.Println("No racks configured")
				return nil
			}

			fmt.Println("Configured racks:")
			for name, gateway := range config.Gateways {
				marker := "  "
				if name == currentRack {
					marker = "* "
				}
				fmt.Printf("%s%s - %s\n", marker, name, gateway.URL)
			}

			return nil
		}),
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show convox-gateway version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("convox-gateway version %s (built %s)\n", Version, BuildTime)
		},
	}

	webCmd := &cobra.Command{
		Use:   "web [rack]",
		Short: "Open the Convox Gateway web UI",
		Args:  cobra.RangeArgs(0, 1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			var rack string
			if len(args) == 1 {
				rack = args[0]
			} else {
				var err error
				rack, err = getCurrentRack()
				if err != nil || rack == "" {
					return fmt.Errorf("no rack selected. Specify a rack: convox-gateway web <rack>")
				}
			}

			gatewayURL, err := loadGatewayURL(rack)
			if err != nil {
				return fmt.Errorf("rack %s not configured. Run: convox-gateway login %s <gateway-url>", rack, rack)
			}
			url := gatewayURL
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				url = "https://" + url
			}
			url = strings.TrimSuffix(url, "/") + "/.gateway/web/"
			fmt.Printf("Opening %s\n", url)
			return openBrowser(url)
		}),
	}

	logoutCmd := &cobra.Command{
		Use:   "logout [rack]",
		Short: "Remove a rack (deletes config and token)",
		Args:  cobra.RangeArgs(0, 1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			var rack string
			if len(args) == 1 {
				rack = args[0]
			} else {
				var err error
				rack, err = getCurrentRack()
				if err != nil || rack == "" {
					return fmt.Errorf("no rack selected. Specify a rack: convox-gateway logout <rack>")
				}
			}

			removed, err := removeRack(rack)
			if err != nil {
				return fmt.Errorf("failed to remove rack: %w", err)
			}

			// If the removed rack was the current rack, unset it
			if cur, _ := getCurrentRack(); cur == rack {
				_ = unsetCurrentRack()
			}

			if removed {
				fmt.Printf("Removed rack: %s\n", rack)
			} else {
				fmt.Printf("Rack not found: %s (nothing to remove)\n", rack)
			}
			return nil
		}),
	}

	switchCmd := &cobra.Command{
		Use:   "switch [rack]",
		Short: "Switch to a different rack",
		Args:  cobra.ExactArgs(1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			rack := args[0]

			// Verify the rack exists in our config
			if _, err := loadGatewayURL(rack); err != nil {
				return fmt.Errorf("rack %s not configured. Run: convox-gateway login %s <gateway-url>", rack, rack)
			}

			if err := setCurrentRack(rack); err != nil {
				return fmt.Errorf("failed to switch rack: %w", err)
			}

			fmt.Printf("Switched to rack: %s\n", rack)
			return nil
		}),
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
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
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

	// Two-step helpers as subcommands of login (visible only under `login --help`)
	var completeState string
	var completeCodeVerifier string
	loginStartCmd := &cobra.Command{
		Use:   "start [rack] [gateway-url]",
		Short: "Start login and print parameters (advanced)",
		Args:  cobra.ExactArgs(2),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			rack := args[0]
			gatewayURL := args[1]
			if err := saveGatewayConfig(rack, gatewayURL); err != nil {
				return fmt.Errorf("failed to save gateway config: %w", err)
			}
			startResp, err := startLogin(gatewayURL)
			if err != nil {
				return fmt.Errorf("failed to start login: %w", err)
			}
			fmt.Printf("AUTH_URL=%s\nSTATE=%s\nCODE_VERIFIER=%s\n", startResp.AuthURL, startResp.State, startResp.CodeVerifier)
			b, _ := json.Marshal(startResp)
			fmt.Printf("JSON=%s\n", string(b))
			return nil
		}),
	}
	loginCompleteCmd := &cobra.Command{
		Use:   "complete [rack] [gateway-url]",
		Short: "Complete login after browser authorization (advanced)",
		Args:  cobra.ExactArgs(2),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			rack := args[0]
			gatewayURL := args[1]
			if completeState == "" || completeCodeVerifier == "" {
				return fmt.Errorf("--state and --code-verifier are required")
			}
			loginResp, err := completeLogin(gatewayURL, "", completeState, completeCodeVerifier)
			if err != nil {
				return fmt.Errorf("failed to complete login: %w", err)
			}
			if err := saveToken(rack, loginResp); err != nil {
				return fmt.Errorf("failed to save token: %w", err)
			}
			if err := setCurrentRack(rack); err != nil {
				return fmt.Errorf("failed to set current rack: %w", err)
			}
			fmt.Printf("Successfully logged in as %s\n", loginResp.Email)
			return nil
		}),
	}
	loginCompleteCmd.Flags().StringVar(&completeState, "state", "", "OAuth state returned by login start")
	loginCompleteCmd.Flags().StringVar(&completeCodeVerifier, "code-verifier", "", "PKCE code verifier from login start")

	loginCmd.AddCommand(loginStartCmd, loginCompleteCmd)

	rootCmd.AddCommand(convoxCmd, loginCmd, switchCmd, rackCmd, racksCmd, versionCmd, logoutCmd, webCmd, completionCmd, envCmd, newAPITokenCommand())

	// Allow config path to be set via environment variable or flag
	defaultConfigPath := getEnv("GATEWAY_CLI_CONFIG_DIR", filepath.Join(homeDir(), ".config", "convox-gateway"))
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "Config directory")

	// Add --rack flag as a global flag for rack selection
	rootCmd.PersistentFlags().StringVar(&rackFlag, "rack", "", "Rack to use (overrides current rack)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func loginCommandWithFlags(args []string, noOpen bool, authFile string) error {
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

	// Always print the auth URL for users and automation
	fmt.Printf("Auth URL: %s\n", startResp.AuthURL)
	if authFile != "" {
		// Write shell-friendly lines; avoid unquoted & parsing issues
		content := fmt.Sprintf("AUTH_URL=%s\nSTATE=%s\nCODE_VERIFIER=%s\n", startResp.AuthURL, startResp.State, startResp.CodeVerifier)
		_ = os.WriteFile(authFile, []byte(content), 0600)
	}
	// Optionally open browser
	if !noOpen {
		fmt.Printf("Opening browser for authentication...\n")
		if err := openBrowser(startResp.AuthURL); err != nil {
			fmt.Printf("Please open this URL in your browser:\n%s\n", startResp.AuthURL)
		}
	}

	// Poll the server for completion; it returns 202 while pending
	var loginResp *LoginResponse
	deadline := time.Now().Add(2 * time.Minute)
	for {
		resp, err := completeLogin(gatewayURL, "", startResp.State, startResp.CodeVerifier)
		if err == nil {
			loginResp = resp
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("login did not complete before timeout: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := saveToken(rack, loginResp); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	// Set this rack as the current rack
	if err := setCurrentRack(rack); err != nil {
		return fmt.Errorf("failed to set current rack: %w", err)
	}

	fmt.Printf("Successfully logged in as %s\n", loginResp.Email)
	fmt.Printf("Token expires at: %s\n", loginResp.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("Current rack set to: %s\n", rack)

	return nil
}

func wrapConvoxCommand(args []string) error {
	// Ban rack uninstall via gateway wrapper
	// Detect the subcommand sequence "rack uninstall" anywhere in the args list
	for i := 0; i+1 < len(args); i++ {
		if strings.EqualFold(args[i], "rack") && strings.EqualFold(args[i+1], "uninstall") {
			return fmt.Errorf("'convox rack uninstall' is disabled in convox-gateway. Use the official Convox CLI directly with a local rack token")
		}
	}

	rack, err := selectedRack()
	if err != nil {
		return err
	}

	// Load gateway config and token for the rack
	gatewayURL := os.Getenv("CONVOX_GATEWAY_URL")
	if strings.TrimSpace(gatewayURL) == "" {
		var err error
		gatewayURL, err = loadGatewayURL(rack)
		if err != nil {
			return fmt.Errorf("rack %s not configured. Run: convox-gateway login %s <gateway-url>", rack, rack)
		}
	}

	normalizedURL, err := normalizeGatewayURL(gatewayURL)
	if err != nil {
		return err
	}

	password := os.Getenv("CONVOX_GATEWAY_API_TOKEN")
	if password == "" {
		token, err := loadToken(rack)
		if err != nil {
			return fmt.Errorf("not logged in to rack %s. Run: convox-gateway login %s %s", rack, rack, gatewayURL)
		}
		if time.Now().After(token.ExpiresAt) {
			return fmt.Errorf("token expired for rack %s. Run: convox-gateway login %s %s", rack, rack, gatewayURL)
		}
		password = token.Token
	}

	var rackURL string
	if strings.HasPrefix(normalizedURL, "http://") {
		rackURL = fmt.Sprintf("http://convox:%s@%s",
			password,
			strings.TrimPrefix(normalizedURL, "http://"))
	} else {
		rackURL = fmt.Sprintf("https://convox:%s@%s",
			password,
			strings.TrimPrefix(normalizedURL, "https://"))
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
	url := fmt.Sprintf("%s/.gateway/api/auth/cli/start", strings.TrimSuffix(parsedURL, "/"))

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
	url := fmt.Sprintf("%s/.gateway/api/auth/cli/complete", strings.TrimSuffix(parsedURL, "/"))

	payload := map[string]string{"state": state, "code_verifier": codeVerifier}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, fmt.Errorf("pending")
	}
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

// fetchEnvViaAPI calls the gateway env API to get masked/unmasked values
func fetchEnvViaAPI(gatewayURL, bearerToken, app, key string, showSecrets bool) (map[string]string, error) {
	base := gatewayURL
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	base = strings.TrimSuffix(base, "/")
	q := url.Values{}
	q.Set("app", app)
	if key != "" {
		q.Set("key", key)
	}
	if showSecrets {
		q.Set("secrets", "true")
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/.gateway/api/env?%s", base, q.Encode()), nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch env: %s", string(b))
	}
	var payload struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Env == nil {
		payload.Env = map[string]string{}
	}
	return payload.Env, nil
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

func resolveRackStatus(now time.Time) (*rackStatus, error) {
	if rack, err := getCurrentRack(); err == nil && strings.TrimSpace(rack) != "" {
		gatewayURL, err := loadGatewayURL(rack)
		if err != nil {
			return nil, fmt.Errorf("rack %s not configured", rack)
		}

		status := &rackStatus{
			Rack:       rack,
			GatewayURL: gatewayURL,
		}

		token, err := loadToken(rack)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "expired") {
				status.StatusLines = append(status.StatusLines, "Status: Token expired")
			} else {
				status.StatusLines = append(status.StatusLines, "Status: Not logged in")
			}
			return status, nil
		}

		if now.After(token.ExpiresAt) {
			status.StatusLines = append(status.StatusLines, "Status: Token expired")
			return status, nil
		}

		status.StatusLines = append(status.StatusLines,
			fmt.Sprintf("Status: Logged in as %s", token.Email))
		status.StatusLines = append(status.StatusLines,
			fmt.Sprintf("Token expires: %s", token.ExpiresAt.Format(time.RFC3339)))
		return status, nil
	}

	envURL := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_URL"))
	if envURL == "" {
		return nil, fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url>")
	}

	label := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_RACK"))
	tokenEnv := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_API_TOKEN"))
	if label == "" {
		if tokenEnv == "" {
			return nil, fmt.Errorf("CONVOX_GATEWAY_API_TOKEN must be set when relying on CONVOX_GATEWAY_URL without a rack name")
		}
		label = "Using CONVOX_GATEWAY_API_TOKEN from environment"
	}

	status := &rackStatus{
		Rack:       label,
		GatewayURL: envURL,
	}

	if tokenEnv == "" {
		status.StatusLines = append(status.StatusLines, "Status: CONVOX_GATEWAY_API_TOKEN not set in environment")
	}

	return status, nil
}

func selectedRack() (string, error) {
	if rackFlag != "" {
		return rackFlag, nil
	}
	if env := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_RACK")); env != "" {
		return env, nil
	}
	if url := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_URL")); url != "" {
		if label := strings.TrimSpace(os.Getenv("CONVOX_GATEWAY_RACK")); label != "" {
			return label, nil
		}
		return "(from environment)", nil
	}
	rack, err := getCurrentRack()
	if err != nil {
		return "", fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url> or use --rack flag")
	}
	if strings.TrimSpace(rack) == "" {
		return "", fmt.Errorf("no rack selected. Run: convox-gateway login <rack> <gateway-url> or use --rack flag")
	}
	return rack, nil
}

func normalizeGatewayURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("invalid gateway url")
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + trimmed
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	return trimmed, nil
}

// removeRack deletes both the gateway config and token for a rack.
// Returns true if the rack existed in either map, false if nothing changed.
func removeRack(rack string) (bool, error) {
	if err := os.MkdirAll(configPath, 0700); err != nil {
		return false, err
	}

	configFile := filepath.Join(configPath, "config.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return false, err
	}

	changed := false
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]Token)
	}

	if _, ok := cfg.Gateways[rack]; ok {
		delete(cfg.Gateways, rack)
		changed = true
	}
	if _, ok := cfg.Tokens[rack]; ok {
		delete(cfg.Tokens, rack)
		changed = true
	}

	if !changed {
		return false, nil
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(configFile, out, 0600); err != nil {
		return false, err
	}
	return true, nil
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

func getCurrentRack() (string, error) {
	currentFile := filepath.Join(configPath, "current")

	data, err := os.ReadFile(currentFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func setCurrentRack(rack string) error {
	if err := os.MkdirAll(configPath, 0700); err != nil {
		return err
	}

	currentFile := filepath.Join(configPath, "current")
	return os.WriteFile(currentFile, []byte(rack), 0600)
}

// unsetCurrentRack removes the current rack selection by deleting the file.
func unsetCurrentRack() error {
	currentFile := filepath.Join(configPath, "current")
	if err := os.Remove(currentFile); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// resolveApp determines the Convox app name using a similar precedence to the Convox CLI:
// 1) explicit flag value
// 2) CONVOX_APP env var
// 3) contents of .convox/app in current or parent directories
// 4) fallback to current directory name
func resolveApp(flagVal string) (string, error) {
	if strings.TrimSpace(flagVal) != "" {
		return flagVal, nil
	}
	if v := os.Getenv("CONVOX_APP"); strings.TrimSpace(v) != "" {
		return v, nil
	}
	// search upwards for .convox/app
	wd, err := os.Getwd()
	if err == nil {
		dir := wd
		for {
			p := filepath.Join(dir, ".convox", "app")
			if data, err := os.ReadFile(p); err == nil {
				name := strings.TrimSpace(string(data))
				if name != "" {
					return name, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir { // reached root
				break
			}
			dir = parent
		}
		// fallback to basename of working directory
		base := filepath.Base(wd)
		if base != "" && base != "." && base != "/" {
			return base, nil
		}
	}
	return "", fmt.Errorf("missing app: use -a <app> or set CONVOX_APP or add .convox/app")
}
