// Package pinentry reads a security key PIN from the user.
//
// When stdin is a terminal the PIN is read there, as before. Without a terminal (for example when an
// AI agent runs the CLI on the user's behalf) the PIN is requested in a native macOS dialog that shows
// which gateway and command the user is approving, and a summary of what the command approves, so the
// user still enters the PIN and touches the key for every step-up.
package pinentry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"

	"github.com/DocSpring/rack-gateway/internal/cli/macdialog"
	"github.com/DocSpring/rack-gateway/internal/cli/terminal"
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
)

// dialogScript shows an alert with the message (its first argument) and a security key PIN field, which has the
// focus, and prints "approve", a line break and the PIN when Approve is clicked (or Return pressed). The warnings
// (its second argument, one per line) show in red above the field, with a red warning icon. `activate` brings the
// dialog to the front: when the CLI runs in the background (from an agent, say) it would otherwise open without
// keyboard focus.
const dialogScript = `
ObjC.import('Cocoa');
function run(argv) {
  var app = $.NSApplication.sharedApplication;
  app.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
  app.activateIgnoringOtherApps(true);

  var field = $.NSSecureTextField.alloc.initWithFrame($.NSMakeRect(0, 0, 240, 24));
  var warnings = warningsView(argv[1], 460);

  var alert = $.NSAlert.alloc.init;
  alert.icon = alertIcon(!!warnings);
  alert.messageText = 'Approve this rack-gateway command?';
  alert.informativeText = argv[0];
  alert.accessoryView = stacked([field, warnings]);
  alert.addButtonWithTitle('Approve');
  alert.addButtonWithTitle('Cancel');
  alert.window.initialFirstResponder = field;
  if (alert.runModal != $.NSAlertFirstButtonReturn) {
    return 'cancel';
  }
  return 'approve\n' + field.stringValue.js;
}
` + macdialog.Helpers

// ErrCanceled is returned when the user cancels the PIN dialog or lets it time out.
var ErrCanceled = errors.New("PIN entry canceled")

// secretFlagWords mark flags whose values can be credentials (e.g. --api-token, --mfa-code, --password).
// The values of other flags (--app, --rack, --commit, ...) are shown, so the user can see what they approve.
var secretFlagWords = []string{"token", "secret", "password", "passwd", "passphrase", "key", "credential", "code"}

// Request describes what the PIN will authorize. It is shown in the dialog.
type Request struct {
	// Gateway is the gateway origin, e.g. https://gateway-us.example.com.
	Gateway string
	// Args is the command line being authorized (normally os.Args).
	Args []string
	// Summary optionally describes what the command approves, one item per line (for example each deploy
	// approval it makes). The dialog lists it above the command line.
	Summary []string
	// Warnings say what the command can do that the person approving it must notice. The dialog shows them in red.
	Warnings []string
}

// ReadPIN asks for the security key PIN on the terminal, or in a macOS dialog when stdin is not a terminal.
func ReadPIN(req Request) (string, error) {
	if fd, ok := terminal.StdinFD(); ok {
		return readFromTerminal(fd, os.Stderr, req.Warnings)
	}
	if runtime.GOOS != "darwin" {
		return "", errors.New("security key PIN required but stdin is not a terminal; run the command in a terminal")
	}
	_, _ = fmt.Fprintln(os.Stderr, "Waiting for the security key PIN in the rack-gateway dialog...")
	return readFromDialog(context.Background(), macdialog.Run, req)
}

func readFromTerminal(fd int, prompt io.Writer, warnings []string) (string, error) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(prompt, "WARNING: "+SingleLine(warning, maxLineLength))
	}
	_, _ = fmt.Fprint(prompt, "Enter your security key PIN: ")
	pinBytes, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to read PIN: %w", err)
	}
	return string(pinBytes), nil
}

// readFromDialog shows the PIN dialog and returns the entered PIN.
func readFromDialog(ctx context.Context, run macdialog.Runner, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dialogTimeout)
	defer cancel()

	stdout, stderr, err := run(ctx, dialogScript, dialogMessage(req), dialogWarnings(req.Warnings))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", ErrCanceled
	}
	if err != nil {
		return "", fmt.Errorf("failed to show PIN dialog: %w: %s", err, strings.TrimSpace(stderr))
	}
	answer, pin, _ := strings.Cut(strings.TrimSuffix(stdout, "\n"), "\n")
	if answer != "approve" {
		return "", ErrCanceled
	}
	if pin == "" {
		return "", errors.New("no PIN entered")
	}
	return pin, nil
}

// dialogWarnings renders the warnings for the dialog, one per line, each made safe for one line (see SingleLine).
func dialogWarnings(warnings []string) string {
	lines := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		lines = append(lines, "⚠ "+SingleLine(warning, maxLineLength))
	}
	return strings.Join(lines, "\n")
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
// on the rack) arguments aren't rack-gateway flags, so any argument with a value is masked. Each argument is
// quoted when it needs to be (see quoteArg), so one argument can't pass for several.
func describeCommand(args []string) string {
	shown := make([]string, 0, len(args))
	maskNext, remoteCommand := false, false
	for i, arg := range args {
		var display string
		switch {
		case i == 0:
			display = "rack-gateway"
		case maskNext:
			display, maskNext = "***", false
		case remoteCommand || !strings.HasPrefix(arg, "-"):
			display = maskAssignment(arg)
		case arg == "--":
			remoteCommand = true
			display = arg
		default:
			display, maskNext = describeFlag(arg)
		}
		shown = append(shown, quoteArg(display))
	}
	return SingleLine(strings.Join(shown, " "), maxCommandLength)
}

// quoteArg quotes an argument (Go syntax, with every character outside printable ASCII escaped) when it is empty
// or contains whitespace, a quote, a backslash or anything other than printable ASCII. Otherwise an argument such
// as "KEY --app staging-web" (or one spaced with a blank-looking character like U+2800) would read as a key
// followed by an --app flag, and the dialog would show a different app from the one the command changes. Plain
// arguments stay unquoted.
func quoteArg(arg string) string {
	needsQuotes := arg == "" || strings.ContainsFunc(arg, func(r rune) bool {
		return r <= ' ' || r > '~' || r == '"' || r == '\'' || r == '\\'
	})
	if needsQuotes {
		return strconv.QuoteToASCII(arg)
	}
	return arg
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
