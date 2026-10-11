// Package touchnotify shows a desktop notification when the CLI waits for a security key touch, so a touch isn't
// missed when the terminal is out of sight (or the CLI runs from a script or an agent's shell).
package touchnotify

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	"github.com/DocSpring/rack-gateway/internal/cli/pinentry"
)

// Title is the notification's title.
const Title = "Touch your security key"

const (
	notifyTimeout   = 3 * time.Second
	maxMessageRunes = 200
)

// notificationScript shows a macOS notification. The title and message are arguments, not part of the script, so
// they can't inject AppleScript.
const notificationScript = `on run argv
	display notification (item 2 of argv) with title (item 1 of argv) sound name "Glass"
end run`

// Show shows a notification with the message, e.g. "To approve api on staging (2 of 4)". It's best effort: on a
// system without a notifier, or when the notifier fails, the terminal message is the only prompt.
func Show(message string) {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	if cmd := command(ctx, runtime.GOOS, exec.LookPath, message); cmd != nil {
		_ = cmd.Run()
	}
}

// command returns the notifier command for the OS: osascript on macOS, notify-send on Linux when it's installed.
func command(
	ctx context.Context, goos string, lookPath func(string) (string, error), message string,
) *exec.Cmd {
	message = pinentry.SingleLine(message, maxMessageRunes)
	switch goos {
	case "darwin":
		return exec.CommandContext(ctx, "osascript", "-e", notificationScript, Title, message)
	case "linux":
		if _, err := lookPath("notify-send"); err != nil {
			return nil
		}
		return exec.CommandContext(ctx, "notify-send", "--app-name=rack-gateway", "--urgency=critical", "--",
			Title, message)
	default:
		return nil
	}
}
