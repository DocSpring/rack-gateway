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
