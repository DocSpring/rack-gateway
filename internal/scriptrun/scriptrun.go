// Package scriptrun is how the CLI sends a local script file with a command (`run --script`, `exec --script`) and
// how the gateway reads it back for the audit log.
//
// A command travels as one header line (the exec or process start's Command header), and the rack splits it into
// words the way a shell would (go-shellquote) and runs them with no shell. A script's line breaks can't go in a
// header, so the script travels base64-encoded, and a fixed sh runner turns it back into a file:
//
//	sh -c '<runner>' rack-gateway-script <name> <base64> <command words...>
//
// The runner decodes the script into a new private directory as <name>, runs the command words with the file's
// path as their last argument (`rails runner /tmp/tmp.X/check.rb`) and removes the directory when it exits. The
// image needs sh, mktemp and base64.
package scriptrun

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// runner is the sh script that writes the script file and runs the command with it. $0 is runnerArgv0, $1 the
// file name and $2 the script in base64; the command words follow. It has no single quotes, so it can be
// single-quoted as one word. The traps make the shell exit (running the EXIT trap) on a hangup, interrupt or
// termination, so the file is removed then too.
const runner = `d=$(mktemp -d) || exit 1; cleanup() { rm -rf "$d"; }; trap cleanup EXIT; ` +
	`trap "exit 129" HUP; trap "exit 130" INT; trap "exit 143" TERM; ` +
	`f="$d/$1"; printf %s "$2" | base64 -d > "$f" || exit 1; shift 2; "$@" "$f"`

// runnerArgv0 names the runner in process listings ($0).
const runnerArgv0 = "rack-gateway-script"

// prefix is what every script command starts with, up to the file name.
const prefix = "sh -c '" + runner + "' " + runnerArgv0 + " "

// MaxNameLength is the longest file name a script can have.
const MaxNameLength = 64

// validName is a file name the runner can write: letters, digits, dots, underscores and hyphens, starting with a
// letter or digit (so never . or ..), and needing no quotes on a command line.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// nameChars are the characters SafeName keeps.
var nameChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SafeName returns local, the base name of a script file on the user's machine, as a name the runner can write:
// other characters become underscores, a name that doesn't start with a letter or digit gets a "script" prefix,
// and it's cut to MaxNameLength characters.
func SafeName(local string) string {
	if i := strings.LastIndexAny(local, `/\`); i >= 0 {
		local = local[i+1:]
	}
	name := nameChars.ReplaceAllString(local, "_")
	if name == "" || !validName.MatchString(name[:1]) {
		name = "script" + name
	}
	if len(name) > MaxNameLength {
		name = name[:MaxNameLength]
	}
	return name
}

// Script is a script command read back from its Command header.
type Script struct {
	// Name is the script's file name.
	Name string
	// Text is the script.
	Text string
	// SHA256 is the hex SHA-256 of the script's bytes.
	SHA256 string
	// Words are the command words that run it, as sent (the rack splits them like a shell).
	Words string
}

// Command is the command as a person would type it to run the script locally: the words, then the file name.
func (s Script) Command() string {
	return s.Words + " " + s.Name
}

// Lines is the number of lines in the script.
func (s Script) Lines() int {
	if s.Text == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s.Text, "\n"), "\n") + 1
}

var (
	errEmpty   = errors.New("the script is empty")
	errNotUTF8 = errors.New("the script isn't UTF-8 text")
	errNUL     = errors.New("the script has NUL bytes: it must be text")
)

// Check refuses a script the gateway wouldn't read back (see Parse): it must be UTF-8 text, not empty, without NUL
// bytes. (An empty script's base64 would be an empty word, which the rack's word splitting drops.)
func Check(script []byte) error {
	if len(script) == 0 {
		return errEmpty
	}
	if !utf8.Valid(script) {
		return errNotUTF8
	}
	if bytes.IndexByte(script, 0) >= 0 {
		return errNUL
	}
	return nil
}

// Build returns the command that runs script, named name (see SafeName), with words, the command words as
// they're sent (joined with spaces). It doesn't check its arguments: Parse reads back only a valid name, a script
// Check accepts, and words that aren't empty.
func Build(name string, script []byte, words string) string {
	return prefix + name + " " + base64.StdEncoding.EncodeToString(script) + " " + words
}

// Parse reads a script command back from command, a Command header. ok is false for anything Build didn't make
// from a valid name, script and words; such a command is just a command.
func Parse(command string) (Script, bool) {
	rest, ok := strings.CutPrefix(command, prefix)
	if !ok {
		return Script{}, false
	}
	name, rest, ok := strings.Cut(rest, " ")
	if !ok || !validName.MatchString(name) {
		return Script{}, false
	}
	encoded, words, ok := strings.Cut(rest, " ")
	if !ok || strings.TrimSpace(words) == "" {
		return Script{}, false
	}
	text, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || Check(text) != nil {
		return Script{}, false
	}
	sum := sha256.Sum256(text)
	return Script{Name: name, Text: string(text), SHA256: hex.EncodeToString(sum[:]), Words: words}, true
}
