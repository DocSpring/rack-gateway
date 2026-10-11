// Package pinentry reads a security key PIN from the user.
//
// When stdin is a terminal the PIN is read there, as before. Without a terminal (for example when an
// AI agent runs the CLI on the user's behalf) the PIN is requested in a native macOS dialog that shows
// which gateway and command the user is approving, and a summary of what the command approves, so the
// user still enters the PIN and touches the key for every step-up.
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
	"unicode"

	"golang.org/x/term"
)

const (
	// dialogTimeout bounds how long the dialog waits for the user before giving up.
	dialogTimeout = 3 * time.Minute
	// maxCommandLength keeps a long command line from overflowing the dialog.
	maxCommandLength = 400
	// maxLineLength keeps each summary line (and the gateway) to about one line of the dialog.
	maxLineLength = 160
	// maxSummaryLines keeps a long summary readable; the lines past it are counted instead of shown.
	maxSummaryLines = 12
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

// secretFlagWords mark flags whose values can be credentials (e.g. --api-token, --mfa-code, --password).
// The values of other flags (--app, --rack, --commit, ...) are shown, so the user can see what they approve.
var secretFlagWords = []string{"token", "secret", "password", "passwd", "passphrase", "key", "credential", "code"}

// dialogRunner runs the AppleScript with the message as its only argument.
type dialogRunner func(ctx context.Context, script, message string) (stdout, stderr string, err error)

// Request describes what the PIN will authorize. It is shown in the dialog.
type Request struct {
	// Gateway is the gateway origin, e.g. https://gateway-us.example.com.
	Gateway string
	// Args is the command line being authorized (normally os.Args).
	Args []string
	// Summary optionally describes what the command approves, one item per line (for example each deploy
	// approval it makes). The dialog lists it above the command line.
	Summary []string
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

// dialogMessage renders the dialog text. Everything that comes from outside the dialog (the summary, the
// gateway, the command line) goes through SingleLine, so it can't add lines or reorder what the dialog shows.
func dialogMessage(req Request) string {
	gateway := strings.TrimPrefix(strings.TrimPrefix(req.Gateway, "https://"), "http://")

	var message strings.Builder
	message.WriteString("Enter your security key PIN to approve this rack-gateway command, then touch the key.\n\n")
	if len(req.Summary) > 0 {
		message.WriteString("Approving:\n" + describeSummary(req.Summary) + "\n\n")
	}
	message.WriteString("Gateway: " + SingleLine(gateway, maxLineLength) + "\n")
	message.WriteString("Command: " + describeCommand(req.Args))
	return message.String()
}

// describeSummary renders the summary as one bullet per line, each cut to maxLineLength. Past
// maxSummaryLines, the remaining lines are counted instead of shown.
func describeSummary(lines []string) string {
	shown := lines
	if len(lines) > maxSummaryLines {
		shown = lines[:maxSummaryLines-1]
	}

	bullets := make([]string, 0, len(shown)+1)
	for _, line := range shown {
		bullets = append(bullets, "• "+SingleLine(line, maxLineLength))
	}
	if hidden := len(lines) - len(shown); hidden > 0 {
		bullets = append(bullets, fmt.Sprintf("• ...and %d more", hidden))
	}
	return strings.Join(bullets, "\n")
}

// describeCommand renders the command line for display with secret values masked: KEY=value arguments
// (e.g. env set, --env KEY=value) and the values of flags that can carry credentials. After "--" (a command run
// on the rack) arguments aren't rack-gateway flags, so any argument with a value is masked.
func describeCommand(args []string) string {
	shown := make([]string, 0, len(args))
	maskNext, remoteCommand := false, false
	for i, arg := range args {
		switch {
		case i == 0:
			shown = append(shown, "rack-gateway")
		case maskNext:
			shown = append(shown, "***")
			maskNext = false
		case remoteCommand || !strings.HasPrefix(arg, "-"):
			shown = append(shown, maskAssignment(arg))
		case arg == "--":
			remoteCommand = true
			shown = append(shown, arg)
		default:
			var flag string
			flag, maskNext = describeFlag(arg)
			shown = append(shown, flag)
		}
	}
	return SingleLine(strings.Join(shown, " "), maxCommandLength)
}

// maskAssignment masks the value of a KEY=value argument.
func maskAssignment(arg string) string {
	if name, _, found := strings.Cut(arg, "="); found {
		return name + "=***"
	}
	return arg
}

// describeFlag renders a flag argument, masking an inline value that can be secret: a secret flag's
// (--api-token=...) or a KEY=value one (--env=KEY=value). maskNext reports that the flag is secret and its value
// is the next argument (--api-token ...).
func describeFlag(arg string) (shown string, maskNext bool) {
	name, value, hasValue := strings.Cut(arg, "=")
	switch {
	case !hasValue:
		return arg, isSecretFlag(name)
	case isSecretFlag(name):
		return name + "=***", false
	default:
		return name + "=" + maskAssignment(value), false
	}
}

func isSecretFlag(name string) bool {
	name = strings.ToLower(strings.TrimLeft(name, "-"))
	for _, word := range secretFlagWords {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

// SingleLine makes text safe for one line of a dialog or notification: control characters (newlines, escape
// sequences), Unicode line and paragraph separators and bidirectional overrides become '?', so the text can't add
// lines or reorder what is shown. Text longer than maxRunes is cut and ends in "...".
func SingleLine(s string, maxRunes int) string {
	runes := []rune(s)
	truncated := len(runes) > maxRunes
	if truncated {
		runes = runes[:maxRunes]
	}
	for i, r := range runes {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Bidi_Control) {
			runes[i] = '?'
		}
	}
	if truncated {
		return string(runes) + "..."
	}
	return string(runes)
}
