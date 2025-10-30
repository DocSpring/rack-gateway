package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// SilenceOnError wraps a command function to silence usage on errors
func SilenceOnError(fn func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		}
		return err
	}
}

// RenderGatewayError formats a gateway API error response
func RenderGatewayError(body []byte) string {
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

// OpenBrowser opens a URL in the user's default browser
func OpenBrowser(url string) error {
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

// IsInteractive returns true if stdin and stdout are both terminals
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// ResolveApp determines the Convox app name using a similar precedence to the Convox CLI:
// 1) explicit flag value
// 2) CONVOX_APP env var
// 3) contents of .convox/app in current or parent directories
// 4) fallback to current directory name
func ResolveApp(flagVal string) (string, error) {
	if val := strings.TrimSpace(flagVal); val != "" {
		return val, nil
	}
	if val := strings.TrimSpace(os.Getenv("CONVOX_APP")); val != "" {
		return val, nil
	}
	return resolveAppFromWorkingDir()
}

func resolveAppFromWorkingDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("missing app: use -a <app> or set CONVOX_APP or add .convox/app")
	}

	if name := searchConvoxApp(wd); name != "" {
		return name, nil
	}

	return fallbackDirName(wd)
}

func searchConvoxApp(start string) string {
	dir := start
	for {
		candidate := filepath.Join(dir, ".convox", "app")
		if data, err := os.ReadFile(candidate); err == nil {
			if name := strings.TrimSpace(string(data)); name != "" {
				return name
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fallbackDirName(path string) (string, error) {
	base := strings.TrimSpace(filepath.Base(path))
	if base != "" && base != "." && base != "/" {
		return base, nil
	}
	return "", fmt.Errorf("missing app: use -a <app> or set CONVOX_APP or add .convox/app")
}
