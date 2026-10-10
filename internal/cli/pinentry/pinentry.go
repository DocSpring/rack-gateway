// Package pinentry reads a security key PIN from the user.
//
// When stdin is a terminal the PIN is read there, as before. Without a terminal (for example when an
// AI agent runs the CLI on the user's behalf) the PIN is requested in a native macOS dialog that shows
// which gateway and command the user is approving, so the user still enters the PIN and touches the key
// for every step-up.
package pinentry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	// dialogTimeout bounds how long the dialog waits for the user before giving up.
	dialogTimeout = 3 * time.Minute
	// maxCommandLength keeps a long command line from overflowing the dialog.
	maxCommandLength = 400
	// appleScriptUserCanceled is the AppleScript error number for a canceled dialog.
	appleScriptUserCanceled = "(-128)"
)

// dialogScript shows the message passed as the first argument (never interpolated into the script)
// and prints the entered PIN to stdout. `activate` brings osascript to the front first: when the CLI
// runs in the background (e.g. from an agent) the dialog otherwise opens without keyboard focus.
const dialogScript = `on run argv
	activate
	set reply to display dialog (item 1 of argv) with title "rack-gateway" default answer "" ¬
		with hidden answer buttons {"Cancel", "Approve"} default button "Approve" cancel button "Cancel" ¬
		with icon caution giving up after 170
	if gave up of reply then error "PIN dialog timed out" number -128
	return text returned of reply
end run`

// ErrCanceled is returned when the user cancels the PIN dialog or lets it time out.
var ErrCanceled = errors.New("PIN entry canceled")

// dialogRunner runs the AppleScript with the message as its only argument.
type dialogRunner func(ctx context.Context, script, message string) (stdout, stderr string, err error)

// Request describes what the PIN will authorize. It is shown in the dialog.
type Request struct {
	// Gateway is the gateway origin, e.g. https://gateway-us.example.com.
	Gateway string
	// Args is the command line being authorized (normally os.Args).
	Args []string
}

// ReadPIN asks for the security key PIN on the terminal, or in a macOS dialog when stdin is not a terminal.
func ReadPIN(req Request) (string, error) {
	if fd, ok := stdinFD(); ok && term.IsTerminal(fd) {
		return readFromTerminal(fd, os.Stderr)
	}
	if runtime.GOOS != "darwin" {
		return "", errors.New("security key PIN required but stdin is not a terminal; run the command in a terminal")
	}
	_, _ = fmt.Fprintln(os.Stderr, "Waiting for the security key PIN in the rack-gateway dialog...")
	return readFromDialog(context.Background(), runOsascript, req)
}

func stdinFD() (int, bool) {
	fd := os.Stdin.Fd()
	const maxInt = int(^uint(0) >> 1)
	if fd > uintptr(maxInt) {
		return 0, false
	}
	return int(fd), true
}

func readFromTerminal(fd int, prompt io.Writer) (string, error) {
	_, _ = fmt.Fprint(prompt, "Enter your security key PIN: ")
	pinBytes, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to read PIN: %w", err)
	}
	return string(pinBytes), nil
}

// readFromDialog shows the PIN dialog and returns the entered PIN.
func readFromDialog(ctx context.Context, run dialogRunner, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dialogTimeout)
	defer cancel()

	stdout, stderr, err := run(ctx, dialogScript, dialogMessage(req))
	if err != nil {
		if strings.Contains(stderr, appleScriptUserCanceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ErrCanceled
		}
		return "", fmt.Errorf("failed to show PIN dialog: %w: %s", err, strings.TrimSpace(stderr))
	}

	pin := strings.TrimSuffix(stdout, "\n")
	if pin == "" {
		return "", errors.New("no PIN entered")
	}
	return pin, nil
}

func runOsascript(ctx context.Context, script, message string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "osascript", "-e", script, message)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func dialogMessage(req Request) string {
	gateway := strings.TrimPrefix(strings.TrimPrefix(req.Gateway, "https://"), "http://")
	return "Enter your security key PIN to approve this rack-gateway command, then touch the key.\n\n" +
		"Gateway: " + gateway + "\n" +
		"Command: " + describeCommand(req.Args)
}

// describeCommand renders the command line for display with secret values masked:
// KEY=value arguments (e.g. env set) and the values of credential flags.
func describeCommand(args []string) string {
	shown := make([]string, 0, len(args))
	maskNext := false
	for i, arg := range args {
		switch {
		case i == 0:
			shown = append(shown, "rack-gateway")
		case maskNext:
			shown = append(shown, "***")
			maskNext = false
		case isCredentialFlag(arg):
			shown = append(shown, arg)
			maskNext = true
		case strings.Contains(arg, "="):
			name, _, _ := strings.Cut(arg, "=")
			shown = append(shown, name+"=***")
		default:
			shown = append(shown, arg)
		}
	}

	command := strings.Join(shown, " ")
	if len(command) > maxCommandLength {
		command = command[:maxCommandLength] + "..."
	}
	return command
}

func isCredentialFlag(arg string) bool {
	return arg == "--api-token" || arg == "--mfa-code"
}
