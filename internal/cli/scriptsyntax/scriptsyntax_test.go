package scriptsyntax

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeRuby puts a ruby on the PATH that answers --version and runs body for anything else. It records its arguments
// and stdin in the directory it returns.
func fakeRuby(t *testing.T, versionExit int, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "ruby 3.4.1 (2024-12-25 revision 48d4efcb85) +PRISM [arm64-darwin24]"
  exit %d
fi
echo "$@" > %q/args
cat > %q/stdin
%s
`, versionExit, dir, dir, body)
	executable := os.FileMode(0o755)
	path := filepath.Join(dir, "ruby")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	require.NoError(t, os.Chmod(path, executable))
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return dir
}

func TestCheckRefusesWhatRubyRefuses(t *testing.T) {
	dir := fakeRuby(t, 0, `echo '/opt/rubies/3.4.1/bin/ruby: -:3: syntax errors found (SyntaxError)' >&2
echo '> 3 | end' >&2
echo '-: -:4: older format' >&2
echo '-:5: oldest format' >&2
exit 1`)

	err := Check(context.Background(), "count.rb", []byte("#!/usr/bin/env -S ruby -s\nputs 1\n"))

	require.EqualError(t, err, `count.rb has a syntax error, according to ruby 3.4.1:
count.rb:3: syntax errors found (SyntaxError)
> 3 | end
count.rb:4: older format
count.rb:5: oldest format`)
	args, readErr := os.ReadFile(filepath.Join(dir, "args")) // #nosec G304 -- the test's own directory
	require.NoError(t, readErr)
	require.Equal(t, "--disable=all -c -\n", string(args))
	stdin, readErr := os.ReadFile(filepath.Join(dir, "stdin")) // #nosec G304 -- the test's own directory
	require.NoError(t, readErr)
	require.Equal(t, "\nputs 1\n", string(stdin), "the #! line is blanked and the line numbers stay")
}

func TestCheckCutsLongOutput(t *testing.T) {
	fakeRuby(t, 0, `i=0; while [ $i -lt 40 ]; do echo "error $i" >&2; i=$((i+1)); done; exit 1`)

	err := Check(context.Background(), "count.rb", []byte("puts 1\n"))

	require.ErrorContains(t, err, "error 29\n(10 more lines)")
	require.NotContains(t, err.Error(), "error 30")
}

func TestCheckLeavesTheScriptUnchecked(t *testing.T) {
	refuses := "echo refused >&2; exit 1"

	t.Run("a Ruby that accepts it", func(t *testing.T) {
		fakeRuby(t, 0, "exit 0")
		require.NoError(t, Check(context.Background(), "count.rb", []byte("puts 1\n")))
	})
	t.Run("another language", func(t *testing.T) {
		dir := fakeRuby(t, 0, refuses)
		for _, name := range []string{"count.py", "count", "rb", "count.rb.txt", "count.RB"} {
			require.NoError(t, Check(context.Background(), name, []byte("(((\n")))
		}
		require.NoFileExists(t, filepath.Join(dir, "args"), "Ruby never runs")
	})
	t.Run("no Ruby", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		require.NoError(t, Check(context.Background(), "count.rb", []byte("(((\n")))
	})
	t.Run("a version manager's shim with no version set", func(t *testing.T) {
		dir := fakeRuby(t, 1, refuses)
		require.NoError(t, Check(context.Background(), "count.rb", []byte("(((\n")))
		require.NoFileExists(t, filepath.Join(dir, "args"), "the check itself never runs")
	})
	t.Run("a Ruby killed by a signal", func(t *testing.T) {
		fakeRuby(t, 0, "kill -9 $$")
		require.NoError(t, Check(context.Background(), "count.rb", []byte("puts 1\n")))
	})
	t.Run("a Ruby that hangs", func(t *testing.T) {
		// Not exec: the sleep outlives the killed shell and holds its output open.
		fakeRuby(t, 0, "sleep 30; exit 1")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		started := time.Now()
		require.NoError(t, Check(ctx, "count.rb", []byte("puts 1\n")))
		require.Less(t, time.Since(started), 10*time.Second)
	})
}

func TestWithoutShebang(t *testing.T) {
	require.Equal(t, "puts 1\n", string(withoutShebang([]byte("puts 1\n"))))
	require.Equal(t, "\nputs 1\n", string(withoutShebang([]byte("#!/bin/sh\nputs 1\n"))))
	require.Empty(t, withoutShebang([]byte("#!/bin/sh")))
	require.Equal(t, " #!/bin/sh\n", string(withoutShebang([]byte(" #!/bin/sh\n"))))
}

// realRuby skips a test when this machine has no Ruby that runs, and otherwise runs it in an empty directory.
func realRuby(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if output, err := exec.CommandContext(context.Background(), "ruby", "--version").Output(); err != nil ||
		len(strings.Fields(string(output))) < 2 {
		t.Skip("no Ruby that runs on this machine")
	}
	return dir
}

func TestCheckWithARealRuby(t *testing.T) {
	realRuby(t)

	require.NoError(t, Check(context.Background(), "count.rb", []byte("puts Account.where(id: 1).count\n")),
		"only the syntax is checked: Account doesn't need to exist")
	require.NoError(t, Check(context.Background(), "count.rb", []byte("puts 1\n__END__\n(((\n")),
		"data after __END__ isn't code")
	require.NoError(t, Check(context.Background(), "count.rb", []byte("#!/bin/sh\nputs 1\n")),
		"a #! line naming another interpreter doesn't stop the check")

	err := Check(context.Background(), "count.rb", []byte("#!/usr/bin/env ruby\nputs 1\ndef count\n  puts (1\nend\n"))
	require.ErrorContains(t, err, "count.rb has a syntax error, according to ruby ")
	require.ErrorContains(t, err, "count.rb:")
	require.NotContains(t, err.Error(), "-:", "names the script, not stdin")
}

// The check runs before anyone has reviewed the script, so nothing in it may run.
func TestCheckRunsNothingInTheScript(t *testing.T) {
	dir := realRuby(t)
	marker := filepath.Join(dir, "ran")
	write := fmt.Sprintf("File.write(%q, 'x')\n", marker)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "planted.rb"), []byte(write), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rubygems.rb"), []byte(write), 0o600))
	t.Setenv("RUBYOPT", "-r./planted")
	t.Setenv("RUBYLIB", dir)

	// Under this env the check does reach Ruby, and running a script does leave the marker: the checks below can fail.
	require.ErrorContains(t, Check(context.Background(), "count.rb", []byte("puts (1\n")), "syntax error")
	require.NoFileExists(t, marker)
	run := exec.CommandContext(context.Background(), "ruby", "--disable=all", "planted.rb")
	require.NoError(t, run.Run())
	require.FileExists(t, marker)
	require.NoError(t, os.Remove(marker))

	for name, script := range map[string]string{
		"the top level":  write,
		"a BEGIN block":  "BEGIN { " + write + " }\n",
		"an END block":   "END { " + write + " }\n",
		"a #! -r switch": "#!ruby -r./planted\nputs 1\n",
		"a #! -e switch": "#!ruby -e " + strings.TrimSpace(write) + "\nputs 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, Check(context.Background(), "count.rb", []byte(script)))
			require.NoFileExists(t, marker)
		})
	}
}
