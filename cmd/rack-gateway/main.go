package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type Config struct {
	Current        string                   `json:"current,omitempty"`
	Gateways       map[string]GatewayConfig `json:"gateways"`
	Tokens         map[string]Token         `json:"tokens"`
	MachineID      string                   `json:"machine_id,omitempty"`
	MFAPreference  string                   `json:"mfa_preference,omitempty"`  // "auto", "webauthn", "totp", "backup_code", or "prompt"
	MFAPreferences map[string]string        `json:"mfa_preferences,omitempty"` // per-rack MFA preferences
}

type GatewayConfig struct {
	URL string `json:"url"`
}

type Token struct {
	Token       string    `json:"token"`
	Email       string    `json:"email"`
	ExpiresAt   time.Time `json:"expires_at"`
	SessionID   int64     `json:"session_id"`
	Channel     string    `json:"channel"`
	DeviceID    string    `json:"device_id,omitempty"`
	DeviceName  string    `json:"device_name,omitempty"`
	MFAVerified bool      `json:"mfa_verified,omitempty"`
}

var ErrLoginPending = errors.New("login pending")

func configFile() string {
	return filepath.Join(configPath, "config.json")
}

func loadConfig() (*Config, bool, error) {
	cfg := &Config{}
	path := configFile()
	data, err := os.ReadFile(path)
	exists := true
	if err != nil {
		if os.IsNotExist(err) {
			exists = false
		} else {
			return nil, false, err
		}
	} else {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, false, err
		}
	}
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]Token)
	}
	if cfg.MFAPreferences == nil {
		cfg.MFAPreferences = make(map[string]string)
	}
	dirty := false
	if strings.TrimSpace(cfg.MachineID) == "" {
		cfg.MachineID = uuid.NewString()
		dirty = true
	}
	if cfg.MFAPreference == "" {
		cfg.MFAPreference = "auto" // Default to auto mode
		dirty = true
	}
	if dirty {
		if err := saveConfig(cfg); err != nil {
			return nil, exists, err
		}
	}
	return cfg, exists, nil
}

func saveConfig(cfg *Config) error {
	if err := os.MkdirAll(configPath, 0700); err != nil {
		return err
	}
	if cfg.Gateways == nil {
		cfg.Gateways = make(map[string]GatewayConfig)
	}
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]Token)
	}
	if cfg.MFAPreferences == nil {
		cfg.MFAPreferences = make(map[string]string)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configFile(), data, 0600); err != nil {
		return err
	}
	// Remove standalone current file if it exists so config.json remains the source of truth.
	_ = os.Remove(filepath.Join(configPath, "current"))
	return nil
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
	Token              string    `json:"token"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	ExpiresAt          time.Time `json:"expires_at"`
	SessionID          int64     `json:"session_id"`
	Channel            string    `json:"channel"`
	DeviceID           string    `json:"device_id"`
	DeviceName         string    `json:"device_name"`
	MFAVerified        bool      `json:"mfa_verified"`
	MFARequired        bool      `json:"mfa_required"`
	EnrollmentRequired bool      `json:"enrollment_required"`
}

type DeviceInfo struct {
	ID            string
	Name          string
	OS            string
	ClientVersion string
}

var (
	configPath   string
	rackFlag     string
	apiTokenFlag string
	Version      = "dev"
	BuildTime    = "unknown"
	httpClient   = &http.Client{Timeout: 30 * time.Second}
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
		Use:           "rack-gateway",
		Short:         "API gateway for Convox with authentication and RBAC",
		SilenceErrors: true,
		Long: `Rack Gateway provides secure authenticated access to Convox racks
with SSO authentication, role-based access control, and audit logging.

To run convox commands through the gateway:
  rack-gateway convox apps
  rack-gateway convox ps
  rack-gateway convox deploy

Recommended aliases for your shell:
  alias cx="rack-gateway convox"   # cx apps, cx ps, cx deploy
  alias cg="rack-gateway"          # cg login, cg switch, cg rack

Rack management:
  rack-gateway rack                # Show current rack
  rack-gateway racks               # List all racks
  rack-gateway switch <rack>       # Switch to a different rack
  rack-gateway login <rack> <url>  # Login to a new rack`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no subcommand is specified, show help
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&apiTokenFlag, "api-token", "", "API token to use for CLI requests (overrides RACK_GATEWAY_API_TOKEN)")

	var noOpen bool
	var authFile string
	loginCmd := &cobra.Command{
		Use:   "login [rack] [gateway-url]",
		Short: "Login to a Convox rack via OAuth",
		Long:  "Authenticate with SSO provider and store token for the specified rack.\n\nExample: rack-gateway login staging https://rack-gateway.example.com",
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
				// If just "rack-gateway convox" is run, show convox help
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
				return fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url>")
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
				return fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url>")
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
			config, exists, err := loadConfig()
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("no configuration found. Run: rack-gateway login <rack> <gateway-url>")
			}

			current := strings.TrimSpace(config.Current)

			if len(config.Gateways) == 0 {
				fmt.Println("No racks configured")
				return nil
			}

			fmt.Println("Configured racks:")
			for name, gateway := range config.Gateways {
				marker := "  "
				if name == current {
					marker = "* "
				}
				fmt.Printf("%s%s - %s\n", marker, name, gateway.URL)
			}

			return nil
		}),
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show rack-gateway version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("rack-gateway version %s (built %s)\n", Version, BuildTime)
		},
	}

	webCmd := &cobra.Command{
		Use:   "web [rack]",
		Short: "Open the Rack Gateway web UI",
		Args:  cobra.RangeArgs(0, 1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			var rack string
			if len(args) == 1 {
				rack = args[0]
			} else {
				var err error
				rack, err = getCurrentRack()
				if err != nil || rack == "" {
					return fmt.Errorf("no rack selected. Specify a rack: rack-gateway web <rack>")
				}
			}

			gatewayURL, err := loadGatewayURL(rack)
			if err != nil {
				return fmt.Errorf("rack %s not configured. Run: rack-gateway login %s <gateway-url>", rack, rack)
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
					return fmt.Errorf("no rack selected. Specify a rack: rack-gateway logout <rack>")
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
				return fmt.Errorf("rack %s not configured. Run: rack-gateway login %s <gateway-url>", rack, rack)
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
  $ source <(rack-gateway completion zsh)
  # To load completions for each session, execute once:
  $ rack-gateway completion zsh > "${fpath[1]}/_rack-gateway"

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
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
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
			deviceInfo := determineDeviceInfo()
			loginResp, err := completeLogin(gatewayURL, completeState, completeCodeVerifier, deviceInfo)
			if err != nil {
				return fmt.Errorf("failed to complete login: %w", err)
			}
			if err := performMFAVerification(gatewayURL, loginResp); err != nil {
				return err
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

	rootCmd.AddCommand(convoxCmd, loginCmd, switchCmd, rackCmd, racksCmd, versionCmd, logoutCmd, webCmd, completionCmd, envCmd, newAPITokenCommand(), newDeployApprovalCommand())

	// Allow config path to be set via environment variable or flag
	defaultConfigPath := getEnv("GATEWAY_CLI_CONFIG_DIR", filepath.Join(homeDir(), ".config", "rack-gateway"))
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

	deviceInfo := determineDeviceInfo()
	// Poll the server for completion; it returns 202 while pending
	var loginResp *LoginResponse
	deadline := time.Now().Add(2 * time.Minute)
	pendingNotified := false
	for {
		resp, err := completeLogin(gatewayURL, startResp.State, startResp.CodeVerifier, deviceInfo)
		if err == nil {
			loginResp = resp
			break
		}
		if errors.Is(err, ErrLoginPending) {
			if !pendingNotified {
				fmt.Println("Waiting for multi-factor authentication to complete in your browser...")
				pendingNotified = true
			}
		} else {
			return fmt.Errorf("login failed: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("login did not complete before timeout")
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := performMFAVerification(gatewayURL, loginResp); err != nil {
		return err
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
			return fmt.Errorf("'convox rack uninstall' is disabled in rack-gateway. Use the official Convox CLI directly with a local rack token")
		}
	}

	rack, err := selectedRack()
	if err != nil {
		return err
	}

	// Load gateway config and token for the rack
	gatewayURL := os.Getenv("RACK_GATEWAY_URL")
	if strings.TrimSpace(gatewayURL) == "" {
		var err error
		gatewayURL, err = loadGatewayURL(rack)
		if err != nil {
			return fmt.Errorf("rack %s not configured. Run: rack-gateway login %s <gateway-url>", rack, rack)
		}
	}

	normalizedURL, err := normalizeGatewayURL(gatewayURL)
	if err != nil {
		return err
	}

	password := os.Getenv("RACK_GATEWAY_API_TOKEN")
	if password == "" {
		token, err := loadToken(rack)
		if err != nil {
			return fmt.Errorf("not logged in to rack %s. Run: rack-gateway login %s %s", rack, rack, gatewayURL)
		}
		if time.Now().After(token.ExpiresAt) {
			return fmt.Errorf("token expired for rack %s. Run: rack-gateway login %s %s", rack, rack, gatewayURL)
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

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // response cleanup

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

func completeLogin(gatewayURL, state, codeVerifier string, device DeviceInfo) (*LoginResponse, error) {
	parsedURL := gatewayURL
	if !strings.HasPrefix(parsedURL, "http://") && !strings.HasPrefix(parsedURL, "https://") {
		parsedURL = "https://" + parsedURL
	}
	url := fmt.Sprintf("%s/.gateway/api/auth/cli/complete", strings.TrimSuffix(parsedURL, "/"))

	payload := map[string]string{
		"state":          state,
		"code_verifier":  codeVerifier,
		"device_id":      device.ID,
		"device_name":    device.Name,
		"device_os":      device.OS,
		"client_version": device.ClientVersion,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // response cleanup

	if resp.StatusCode == http.StatusAccepted {
		return nil, ErrLoginPending
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s", renderGatewayError(body))
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func performMFAVerification(gatewayURL string, resp *LoginResponse) error {
	if resp == nil {
		return fmt.Errorf("missing login response for MFA verification")
	}
	if resp.EnrollmentRequired {
		normalized, err := normalizeGatewayURL(gatewayURL)
		if err != nil {
			normalized = gatewayURL
		}
		fmt.Fprintln(os.Stderr, "MFA enrollment is required before using this gateway.")
		fmt.Fprintf(os.Stderr, "Visit %s/.gateway/web to finish setup, then rerun login.\n", normalized)
		return fmt.Errorf("mfa enrollment required")
	}
	if !resp.MFARequired {
		return nil
	}
	if !isInteractive() {
		return fmt.Errorf("MFA verification required; run the login command from an interactive terminal")
	}
	normalized, err := normalizeGatewayURL(gatewayURL)
	if err != nil {
		return err
	}

	// Get current rack to check for per-rack preference
	rack, _ := getCurrentRack()

	// Check what MFA methods user has enrolled
	mfaStatus, err := getMFAStatus(normalized, resp.Token)
	if err != nil {
		return fmt.Errorf("failed to check MFA status: %w", err)
	}

	if len(mfaStatus.Methods) == 0 {
		return fmt.Errorf("no MFA methods enrolled")
	}

	// Determine which method to use based on preference
	cfg, _, err := loadConfig()
	if err != nil {
		cfg = &Config{MFAPreference: "auto"}
	}

	preference := cfg.MFAPreference
	if rack != "" && cfg.MFAPreferences != nil {
		if rackPref, ok := cfg.MFAPreferences[rack]; ok {
			preference = rackPref
		}
	}

	// Try methods based on preference
	if preference == "prompt" || (preference == "auto" && len(mfaStatus.Methods) > 1) {
		// Let user select
		return promptAndVerifyMFAMethod(normalized, resp, mfaStatus.Methods)
	}

	// Auto mode with single method, or specific preference
	methods := filterMethodsByPreference(mfaStatus.Methods, preference)

	for _, method := range methods {
		if err := tryMFAMethod(normalized, resp.Token, method); err == nil {
			fmt.Printf("MFA verified successfully with %s.\n", method.Type)
			resp.MFAVerified = true
			resp.MFARequired = false
			return nil
		}
		// Try next method
	}

	// If all methods failed, prompt user to try manually
	return promptAndVerifyMFAMethod(normalized, resp, mfaStatus.Methods)
}

func submitMFAVerification(baseURL, sessionToken, code string) error {
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/verify", strings.TrimSuffix(baseURL, "/"))
	payload := map[string]any{
		"code": code,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // cleanup best-effort
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("gateway error: %s", renderGatewayError(bodyBytes))
}

func promptMFACode() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Print("Enter MFA code (TOTP, Yubikey OTP, or backup code): ")
		codeBytes, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(codeBytes)), nil
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter MFA code (TOTP, Yubikey OTP, or backup code): ")
	code, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(code), nil
}

type mfaStatusResponse struct {
	Enrolled bool                `json:"enrolled"`
	Required bool                `json:"required"`
	Methods  []mfaMethodResponse `json:"methods"`
}

type mfaMethodResponse struct {
	ID          int64  `json:"id"`
	Type        string `json:"type"`
	Label       string `json:"label"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
	IsEnrolling bool   `json:"is_enrolling"`
}

func getMFAStatus(baseURL, sessionToken string) (*mfaStatusResponse, error) {
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/status", strings.TrimSuffix(baseURL, "/"))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // cleanup best-effort

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway error: %s", renderGatewayError(bodyBytes))
	}

	var status mfaStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode MFA status: %w", err)
	}

	return &status, nil
}

func tryWebAuthnVerification(baseURL, sessionToken string) error {
	// Get assertion challenge from gateway
	endpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/webauthn/assertion/start", strings.TrimSuffix(baseURL, "/"))
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // cleanup best-effort

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to start assertion: %s", renderGatewayError(bodyBytes))
	}

	var startResp struct {
		Options struct {
			Challenge        string `json:"challenge"`
			Timeout          int    `json:"timeout"`
			RPID             string `json:"rpId"`
			AllowCredentials []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"allowCredentials"`
			UserVerification string `json:"userVerification"`
		} `json:"options"`
		SessionData string `json:"session_data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return fmt.Errorf("failed to decode assertion start response: %w", err)
	}

	// Extract allowed credential IDs
	var allowedCreds []string
	for _, cred := range startResp.Options.AllowCredentials {
		allowedCreds = append(allowedCreds, cred.ID)
	}

	// Get origin from base URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse gateway URL: %w", err)
	}
	origin := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// Perform WebAuthn assertion
	assertionOpts := webauthn.AssertionOptions{
		Challenge:        startResp.Options.Challenge,
		RPID:             startResp.Options.RPID,
		AllowCredentials: allowedCreds,
		Timeout:          startResp.Options.Timeout,
		UserVerification: startResp.Options.UserVerification,
		Origin:           origin,
	}

	assertion, err := webauthn.GetAssertion(assertionOpts)
	if err != nil {
		return fmt.Errorf("WebAuthn assertion failed: %w", err)
	}

	// Serialize assertion response
	assertionJSON, err := json.Marshal(assertion)
	if err != nil {
		return fmt.Errorf("failed to marshal assertion: %w", err)
	}

	// Submit assertion to gateway
	verifyEndpoint := fmt.Sprintf("%s/.gateway/api/auth/mfa/webauthn/assertion/verify", strings.TrimSuffix(baseURL, "/"))
	verifyPayload := map[string]any{
		"session_data":       startResp.SessionData,
		"assertion_response": string(assertionJSON),
		"trust_device":       false,
	}

	verifyBody, err := json.Marshal(verifyPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal verify payload: %w", err)
	}

	verifyReq, err := http.NewRequest(http.MethodPost, verifyEndpoint, bytes.NewReader(verifyBody))
	if err != nil {
		return err
	}
	verifyReq.Header.Set("Authorization", "Bearer "+sessionToken)
	verifyReq.Header.Set("Content-Type", "application/json")

	verifyResp, err := httpClient.Do(verifyReq)
	if err != nil {
		return err
	}
	defer verifyResp.Body.Close() //nolint:errcheck // cleanup best-effort

	if verifyResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(verifyResp.Body)
		return fmt.Errorf("assertion verification failed: %s", renderGatewayError(bodyBytes))
	}

	return nil
}

// filterMethodsByPreference returns methods sorted by preference
func filterMethodsByPreference(methods []mfaMethodResponse, preference string) []mfaMethodResponse {
	if preference == "auto" {
		// Auto mode: try WebAuthn first if device available, then TOTP, then backup codes
		var ordered []mfaMethodResponse
		for _, m := range methods {
			if m.Type == "webauthn" && !m.IsEnrolling && webauthn.CheckAvailability() {
				ordered = append(ordered, m)
			}
		}
		for _, m := range methods {
			if m.Type == "totp" && !m.IsEnrolling {
				ordered = append(ordered, m)
			}
		}
		for _, m := range methods {
			if m.Type == "backup_code" && !m.IsEnrolling {
				ordered = append(ordered, m)
			}
		}
		return ordered
	}

	// Specific preference - filter to only that type
	var filtered []mfaMethodResponse
	for _, m := range methods {
		if m.Type == preference && !m.IsEnrolling {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// tryMFAMethod attempts to verify using a specific MFA method
func tryMFAMethod(baseURL, sessionToken string, method mfaMethodResponse) error {
	switch method.Type {
	case "webauthn":
		if !webauthn.CheckAvailability() {
			return fmt.Errorf("no WebAuthn device detected")
		}
		return tryWebAuthnVerification(baseURL, sessionToken)
	case "totp", "backup_code":
		// For TOTP and backup codes, we need to prompt the user
		return fmt.Errorf("interactive prompt required for %s", method.Type)
	default:
		return fmt.Errorf("unsupported MFA method: %s", method.Type)
	}
}

// promptAndVerifyMFAMethod lets user select and verify an MFA method interactively
func promptAndVerifyMFAMethod(baseURL string, resp *LoginResponse, methods []mfaMethodResponse) error {
	if len(methods) == 0 {
		return fmt.Errorf("no MFA methods available")
	}

	// If only one method, use it directly
	if len(methods) == 1 {
		return promptSingleMethod(baseURL, resp, methods[0])
	}

	// Multiple methods - let user choose
	fmt.Println("\nAvailable MFA methods:")
	for i, method := range methods {
		label := method.Label
		if label == "" {
			label = method.Type
		}
		fmt.Printf("%d. %s (%s)\n", i+1, label, method.Type)
	}

	reader := bufio.NewReader(os.Stdin)
	for attempts := 0; attempts < 3; attempts++ {
		fmt.Print("\nSelect method (1-", len(methods), "): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		var choice int
		if _, err := fmt.Sscanf(strings.TrimSpace(input), "%d", &choice); err != nil || choice < 1 || choice > len(methods) {
			fmt.Println("Invalid choice, please try again.")
			continue
		}

		method := methods[choice-1]
		if err := promptSingleMethod(baseURL, resp, method); err == nil {
			return nil
		}
		fmt.Printf("Verification failed: %v\n", err)
		fmt.Println("Try another method or press Ctrl+C to cancel.")
	}

	return fmt.Errorf("failed to verify MFA after multiple attempts")
}

// promptSingleMethod handles verification for a single method
func promptSingleMethod(baseURL string, resp *LoginResponse, method mfaMethodResponse) error {
	switch method.Type {
	case "webauthn":
		if !webauthn.CheckAvailability() {
			return fmt.Errorf("no WebAuthn device detected")
		}
		if err := tryWebAuthnVerification(baseURL, resp.Token); err != nil {
			return err
		}
		fmt.Println("MFA verified successfully with WebAuthn.")
		resp.MFAVerified = true
		resp.MFARequired = false
		return nil

	case "totp":
		fmt.Printf("\nEnter TOTP code from %s: ", method.Label)
		code, err := promptMFACode()
		if err != nil {
			return err
		}
		if err := submitMFAVerification(baseURL, resp.Token, code); err != nil {
			return err
		}
		fmt.Println("MFA verified successfully with TOTP.")
		resp.MFAVerified = true
		resp.MFARequired = false
		return nil

	case "backup_code":
		fmt.Printf("\nEnter backup code: ")
		code, err := promptMFACode()
		if err != nil {
			return err
		}
		if err := submitMFAVerification(baseURL, resp.Token, code); err != nil {
			return err
		}
		fmt.Println("MFA verified successfully with backup code.")
		resp.MFAVerified = true
		resp.MFARequired = false
		return nil

	default:
		return fmt.Errorf("unsupported MFA method: %s", method.Type)
	}
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
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
	defer resp.Body.Close() //nolint:errcheck // response cleanup
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch env: %s", renderGatewayError(b))
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

func renderGatewayError(body []byte) string {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return "forbidden"
	}
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if trimmed := strings.TrimSpace(parsed.Error); trimmed != "" {
			return trimmed
		}
	}
	return msg
}

func saveToken(rack string, loginResp *LoginResponse) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Tokens[rack] = Token{
		Token:       loginResp.Token,
		Email:       loginResp.Email,
		ExpiresAt:   loginResp.ExpiresAt,
		SessionID:   loginResp.SessionID,
		Channel:     loginResp.Channel,
		DeviceID:    loginResp.DeviceID,
		DeviceName:  loginResp.DeviceName,
		MFAVerified: loginResp.MFAVerified,
	}
	return saveConfig(cfg)
}

func loadToken(rack string) (*Token, error) {
	cfg, exists, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("no configuration found")
	}
	token, ok := cfg.Tokens[rack]
	if !ok {
		return nil, fmt.Errorf("no token found for rack: %s", rack)
	}

	if time.Now().After(token.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return &token, nil
}

func saveGatewayConfig(rack, gatewayURL string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Gateways[rack] = GatewayConfig{URL: gatewayURL}
	return saveConfig(cfg)
}

func loadGatewayURL(rack string) (string, error) {
	cfg, exists, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("no configuration found")
	}
	gateway, ok := cfg.Gateways[rack]
	if !ok {
		return "", fmt.Errorf("no gateway configured for rack: %s", rack)
	}

	return gateway.URL, nil
}

func resolveRackStatus(now time.Time) (*rackStatus, error) {
	cfg, exists, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if exists {
		rack := strings.TrimSpace(cfg.Current)
		if rack != "" {
			gateway, ok := cfg.Gateways[rack]
			if !ok {
				return nil, fmt.Errorf("rack %s not configured", rack)
			}

			status := &rackStatus{
				Rack:       rack,
				GatewayURL: gateway.URL,
			}

			token, ok := cfg.Tokens[rack]
			if !ok {
				status.StatusLines = append(status.StatusLines, "Status: Not logged in")
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
			if !token.MFAVerified {
				status.StatusLines = append(status.StatusLines, "MFA: verification required (run an interactive login)")
			}
			return status, nil
		}
	}

	envURL := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL"))
	if envURL == "" {
		return nil, fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url>")
	}

	label := strings.TrimSpace(os.Getenv("RACK_GATEWAY_RACK"))
	tokenEnv := strings.TrimSpace(os.Getenv("RACK_GATEWAY_API_TOKEN"))
	if label == "" {
		if tokenEnv == "" {
			return nil, fmt.Errorf("RACK_GATEWAY_API_TOKEN must be set when relying on RACK_GATEWAY_URL without a rack name")
		}
		label = "Using RACK_GATEWAY_API_TOKEN from environment"
	}

	status := &rackStatus{
		Rack:       label,
		GatewayURL: envURL,
	}

	if tokenEnv == "" {
		status.StatusLines = append(status.StatusLines, "Status: RACK_GATEWAY_API_TOKEN not set in environment")
	}

	return status, nil
}

func selectedRack() (string, error) {
	if rackFlag != "" {
		return rackFlag, nil
	}
	if env := strings.TrimSpace(os.Getenv("RACK_GATEWAY_RACK")); env != "" {
		return env, nil
	}
	if url := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL")); url != "" {
		if label := strings.TrimSpace(os.Getenv("RACK_GATEWAY_RACK")); label != "" {
			return label, nil
		}
		return "(from environment)", nil
	}
	rack, err := getCurrentRack()
	if err != nil {
		return "", fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url> or use --rack flag")
	}
	if strings.TrimSpace(rack) == "" {
		return "", fmt.Errorf("no rack selected. Run: rack-gateway login <rack> <gateway-url> or use --rack flag")
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
	cfg, exists, err := loadConfig()
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	changed := false
	if _, ok := cfg.Gateways[rack]; ok {
		delete(cfg.Gateways, rack)
		changed = true
	}
	if _, ok := cfg.Tokens[rack]; ok {
		delete(cfg.Tokens, rack)
		changed = true
	}
	if cfg.Current == rack {
		cfg.Current = ""
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := saveConfig(cfg); err != nil {
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
	cfg, exists, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !exists || strings.TrimSpace(cfg.Current) == "" {
		return "", fmt.Errorf("no current rack configured")
	}
	return strings.TrimSpace(cfg.Current), nil
}

func setCurrentRack(rack string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Current = rack
	return saveConfig(cfg)
}

// unsetCurrentRack removes the current rack selection by deleting the file.
func unsetCurrentRack() error {
	cfg, exists, err := loadConfig()
	if err != nil {
		return err
	}
	if !exists || strings.TrimSpace(cfg.Current) == "" {
		return nil
	}
	cfg.Current = ""
	return saveConfig(cfg)
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
func determineDeviceInfo() DeviceInfo {
	cfg, _, err := loadConfig()
	deviceID := ""
	if err == nil && cfg != nil {
		deviceID = strings.TrimSpace(cfg.MachineID)
	}
	if deviceID == "" {
		deviceID = uuid.NewString()
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-device"
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = fmt.Sprintf("gateway-cli-%s", runtime.GOOS)
	}

	clientVersion := strings.TrimSpace(Version)
	if clientVersion == "" {
		clientVersion = "dev"
	}

	return DeviceInfo{
		ID:            deviceID,
		Name:          hostname,
		OS:            runtime.GOOS,
		ClientVersion: clientVersion,
	}
}
