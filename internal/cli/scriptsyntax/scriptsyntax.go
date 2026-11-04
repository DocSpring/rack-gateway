// Package scriptsyntax checks a script's syntax locally before the CLI sends it, so that a typo is refused before
// anyone reviews the script or touches a security key.
package scriptsyntax

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// checkTimeout bounds the check: a Ruby that doesn't answer in time leaves the script unchecked.
	checkTimeout = 10 * time.Second
	// killWait bounds the wait for a killed Ruby's output, which a process it started can hold open.
	killWait = time.Second
	// maxOutputLines caps Ruby's output in the error: a script can have many syntax errors.
	maxOutputLines = 30
)

// stdinLocation is how Ruby names the script it read from stdin, at the start of a message, with whatever comes
// before it: the path of the Ruby that's running (3.4), or "-: " (3.3).
var stdinLocation = regexp.MustCompile(`(?m)^(?:\S*ruby: |-: )?-:(\d+)`)

// Check returns an error when the local Ruby refuses the syntax of text, the script in the file called name. It
// only checks .rb files. A machine without a Ruby that runs leaves the script unchecked, as does a Ruby that fails
// any other way than by exiting with an error.
//
// This runs before anyone has reviewed the script, so nothing in it may run: -c only parses, BEGIN blocks included.
// --disable=all ignores RUBYOPT and leaves out the gems, and the script goes in on stdin, as the exact bytes that
// will be sent, with any #! line blanked (see withoutShebang).
func Check(ctx context.Context, name string, text []byte) error {
	if filepath.Ext(name) != ".rb" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	// A version manager's shim also exits with an error when no version is set, so this comes first: that isn't a
	// syntax error.
	version, ok := rubyVersion(ctx)
	if !ok {
		return nil
	}
	cmd := rubyCommand(ctx, "--disable=all", "-c", "-")
	cmd.Stdin = bytes.NewReader(withoutShebang(text))
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() <= 0 {
		return nil
	}
	return fmt.Errorf("%s has a syntax error, according to %s:\n%s", name, version, describe(output, name))
}

// rubyVersion returns the local Ruby's name and version ("ruby 3.4.1"), or false if it doesn't run.
func rubyVersion(ctx context.Context) (string, bool) {
	output, err := rubyCommand(ctx, "--version").Output()
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return "", false
	}
	return fields[0] + " " + fields[1], true
}

func rubyCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "ruby", args...)
	cmd.WaitDelay = killWait
	return cmd
}

// withoutShebang blanks a #! first line and keeps the line numbers. Ruby takes switches from that line, and when it
// names another interpreter Ruby looks for a Ruby script further down, and only parsing is wanted here.
func withoutShebang(text []byte) []byte {
	if !bytes.HasPrefix(text, []byte("#!")) {
		return text
	}
	if lineEnd := bytes.IndexByte(text, '\n'); lineEnd >= 0 {
		return text[lineEnd:]
	}
	return nil
}

// describe is Ruby's output as the error shows it: naming the script, and cut to maxOutputLines. name is safe to use
// in a replacement: scriptrun.SafeName leaves no $.
func describe(output []byte, name string) string {
	message := stdinLocation.ReplaceAllString(strings.TrimSpace(string(output)), name+":$1")
	lines := strings.Split(message, "\n")
	if extra := len(lines) - maxOutputLines; extra > 0 {
		lines = append(lines[:maxOutputLines], fmt.Sprintf("(%d more lines)", extra))
	}
	return strings.Join(lines, "\n")
}
