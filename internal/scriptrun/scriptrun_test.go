package scriptrun

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/require"
)

// tricky has everything a shell or a header could mangle: quotes, $, backticks, backslashes, CRLF, tabs,
// non-ASCII and no trailing newline.
const tricky = "puts 'it''s' \"$HOME\" `id` \\n\r\n\tputs \"naïve — 日本\"\n# end"

// The rack splits the command into words like a shell; the runner, its name, the file name and the base64 come out
// as separate words, followed by the command words.
func TestBuildSplitsIntoTheRunnersArguments(t *testing.T) {
	command := Build("check.rb", []byte(tricky), "rails runner")
	words, err := shellquote.Split(command)
	require.NoError(t, err)
	require.Len(t, words, 8)
	require.Equal(t, []string{"sh", "-c", runner, runnerArgv0, "check.rb"}, words[:5])
	require.Equal(t, []string{"rails", "runner"}, words[6:])
}

func TestParseReadsBackWhatBuildMade(t *testing.T) {
	script, ok := Parse(Build("check.rb", []byte(tricky), "rails runner"))
	require.True(t, ok)
	require.Equal(t, "check.rb", script.Name)
	require.Equal(t, tricky, script.Text)
	require.Equal(t, "rails runner", script.Words)
	require.Equal(t, "rails runner check.rb", script.Command())
	require.Equal(t, 3, script.Lines())
	require.Len(t, script.SHA256, 64)
}

// Parse reads back only what Build makes from a valid name, a script Check accepts and some command words.
// Anything else is a command like any other.
func TestParseRefusesAnythingElse(t *testing.T) {
	valid := Build("check.rb", []byte("puts"), "rails runner") // "puts" is cHV0cw==
	_, ok := Parse(valid)
	require.True(t, ok)
	for name, command := range map[string]string{
		"another runner":    strings.Replace(valid, "mktemp -d", "mktemp", 1),
		"a name with a dot": Build("..", []byte("puts 1"), "rails runner"),
		"a quoted name":     Build("'check.rb'", []byte("puts 1"), "rails runner"),
		"no command words":  Build("check.rb", []byte("puts 1"), " "),
		"non-strict base64": strings.Replace(valid, "cHV0cw==", "cHV0cx==", 1),
		"invalid base64":    strings.Replace(valid, "cHV0cw==", "cHV0c!==", 1),
		"invalid UTF-8":     Build("check.rb", []byte{0xff, 0xfe}, "rails runner"),
		"a NUL byte":        Build("check.rb", []byte("puts 1\x00"), "rails runner"),
		"an empty script":   Build("check.rb", nil, "rails runner"),
		"a plain command":   "rails runner 'puts 1'",
	} {
		_, ok := Parse(command)
		require.False(t, ok, name)
	}
}

func TestSafeName(t *testing.T) {
	for local, want := range map[string]string{
		"check.rb":                      "check.rb",
		"/Users/me/scripts/check.rb":    "check.rb",
		`C:\scripts\check.rb`:           "check.rb",
		"fix $HOME (1).rb":              "fix__HOME__1_.rb",
		".hidden.rb":                    "script.hidden.rb",
		"-rf":                           "script-rf",
		"":                              "script",
		strings.Repeat("a", 70) + ".rb": strings.Repeat("a", 64),
	} {
		name := SafeName(local)
		require.Equal(t, want, name, local)
		require.True(t, validName.MatchString(name), name)
	}
}

// runScript runs Build's command the way the rack does: split into words, run without a shell.
func runScript(t *testing.T, script []byte, words string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("needs base64")
	}
	argv, err := shellquote.Split(Build("check.rb", script, words))
	require.NoError(t, err)
	// Under the strictest umask, which a container's user may have.
	argv = append([]string{"sh", "-c", `umask 077; exec "$@"`, "sh"}, argv...)
	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- the test runs the command it built
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := cmd.Output()
	return string(out), err
}

// The command gets a file holding exactly the script, named as sent, and the file is gone afterwards.
func TestRunnerWritesTheScriptAndRemovesIt(t *testing.T) {
	out, err := runScript(t, []byte(tricky), `sh -c 'echo "$1"; cat "$1"' --`)
	require.NoError(t, err)
	path, content, ok := strings.Cut(out, "\n")
	require.True(t, ok, out)
	require.Equal(t, "check.rb", filepath.Base(path))
	require.Equal(t, tricky, content)
	_, err = os.Stat(filepath.Dir(path))
	require.True(t, os.IsNotExist(err), "the script's directory is removed: %v", err)
}

// Every user can read the script, and enter its directory but not list it: the command may drop to another user
// (`run_command deploy ... rails runner`), which must read the file.
func TestRunnerLetsOtherUsersReadTheScript(t *testing.T) {
	out, err := runScript(t, []byte("puts 1"), `sh -c 'find "$1" -perm 0644; find "${1%/*}" -perm 0711' --`)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2, "the file is 0644 and its directory 0711: %q", out)
	require.Equal(t, filepath.Dir(lines[0]), lines[1])
}

// The command's exit status is the run's.
func TestRunnerKeepsTheCommandsExitStatus(t *testing.T) {
	_, err := runScript(t, []byte("puts 1"), "sh -c 'exit 3'")
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "%v", err)
	require.Equal(t, 3, exitErr.ExitCode())
}
