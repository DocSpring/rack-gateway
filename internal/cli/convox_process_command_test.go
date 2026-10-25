package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/convox/convox/pkg/structs"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cli/scriptreview"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

const testJustification = "Count the templates of account 42 — checking a support ticket"

// processCobraCommand returns cmd (run or exec) for app myapp, with flags set.
func processCobraCommand(t *testing.T, cmd *cobra.Command, flags map[string]string) *cobra.Command {
	t.Helper()
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Flags().Set("app", "myapp"))
	for name, value := range flags {
		require.NoError(t, cmd.Flags().Set(name, value))
	}
	return cmd
}

// Without --justification, a command in an app that requires one is refused before anything is sent to the rack
// (the gateway's refusal of an exec would only show as "websocket: bad handshake").
func TestProcessCommandRefusedWithoutAJustificationTheAppRequires(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	gateway.appSettings = `{"require_command_justification":{"value":true}}`
	for _, cmd := range []*cobra.Command{ExecCommand(), RunCommand()} {
		cmd = processCobraCommand(t, cmd, nil)
		_, _, err := setupProcessCommand(cmd, []string{"web", "rails", "runner", "puts 1"}, cmd.Name())
		require.ErrorContains(t, err, "app myapp requires a justification for every command")
	}
	require.Empty(t, gateway.requests())
}

// A justification that can't say why the command is run is refused. A good one goes with each of the command's
// requests, as the header the gateway records.
func TestProcessCommandSendsItsJustification(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	MFACodeFlag = "123456"
	args := []string{"web", "rails", "runner", "puts 1"}

	short := processCobraCommand(t, RunCommand(), map[string]string{justificationFlag: "checking"})
	_, _, err := setupProcessCommand(short, args, "run")
	require.ErrorContains(t, err, "the justification is too short")

	cmd := processCobraCommand(t, RunCommand(), map[string]string{justificationFlag: testJustification})
	client, _, err := setupProcessCommand(cmd, args, "run")
	require.NoError(t, err)
	placeholder := convox.AttachedRunPlaceholder
	_, _ = client.ProcessRun("myapp", "web", structs.ProcessRunOptions{Command: &placeholder})

	proxied := gateway.requests()
	require.Len(t, proxied, 1)
	require.Equal(t, audit.EncodeJustification(testJustification), proxied[0].Get(audit.JustificationHeader))
}

// The PIN dialog shows where the command runs, the command and its justification, and a script's name, size and
// SHA-256 (the script itself is reviewed before the PIN dialog).
func TestCommandPromptDescribesTheCommand(t *testing.T) {
	process := processCommand{
		action: "run", app: "myapp", rack: "us", target: "web", command: "rails runner 'puts 1'",
		justification: testJustification,
	}
	prompt := commandPrompt(process)
	require.Equal(t, []string{
		"On app myapp (rack us), run in a new web process: rails runner 'puts 1'",
		"Justification: " + testJustification,
	}, prompt.Summary)
	require.Equal(t, "run a command on app myapp (rack us)", prompt.Touch)

	prompt = commandPrompt(processCommand{action: "exec", app: "myapp", rack: "us", target: "p-1", command: "ls"})
	require.Equal(t, []string{"On app myapp (rack us), exec in process p-1: ls"}, prompt.Summary)

	script, ok := scriptrun.Parse(scriptrun.Build("count.rb", []byte("puts 1\nputs 2\n"), "rails runner"))
	require.True(t, ok)
	process.command, process.script = script.Command(), &script
	require.Equal(t, []string{
		"On app myapp (rack us), run in a new web process: rails runner count.rb",
		"Justification: " + testJustification,
		"Script: count.rb, 2 lines, SHA-256 " + script.SHA256,
	}, commandPrompt(process).Summary)
}

// writeScript writes text to a file named name in a new directory and returns its path.
func writeScript(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	return path
}

// reviewedScripts replaces the script review with one that records each script and answers with err.
func reviewedScripts(t *testing.T, err error) *[]processCommand {
	t.Helper()
	var reviewed []processCommand
	previous := reviewScriptWith
	reviewScriptWith = func(process processCommand) error {
		reviewed = append(reviewed, process)
		return err
	}
	t.Cleanup(func() { reviewScriptWith = previous })
	return &reviewed
}

// With --script, the command sent is the script command (scriptrun.Build) made from the command words and the
// file, once the script is approved. The rest of the CLI gets it as the command's only word.
func TestProcessCommandSendsAnApprovedScript(t *testing.T) {
	newEnvChangeGateway(t)
	reviewed := reviewedScripts(t, nil)
	text := "accounts = Account.where(plan: 'pro')\nputs accounts.count\n"
	path := writeScript(t, "count pro.rb", text)
	for _, cmd := range []*cobra.Command{ExecCommand(), RunCommand()} {
		cmd = processCobraCommand(t, cmd, map[string]string{scriptFlag: path, justificationFlag: testJustification})
		_, ctx, err := setupProcessCommand(cmd, []string{"web", "rails", "runner"}, cmd.Name())
		require.NoError(t, err)
		require.Equal(t, []string{"web", scriptrun.Build("count_pro.rb", []byte(text), "rails runner")}, ctx.Args)
	}
	require.Len(t, *reviewed, 2)
	process := (*reviewed)[1]
	require.Equal(t, "rails runner count_pro.rb", process.shownCommand())
	require.Equal(t, text, process.script.Text)
	require.Equal(t, testJustification, process.justification)
	require.Equal(t, "app myapp (rack us)", process.where())
}

// A script that isn't approved isn't sent, and an API token (CI) has no one to approve it, so it's sent unreviewed.
func TestProcessCommandScriptReview(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	reviewed := reviewedScripts(t, scriptreview.ErrDeclined)
	flags := map[string]string{scriptFlag: writeScript(t, "count.rb", "puts 1\n"), justificationFlag: testJustification}
	args := []string{"web", "rails", "runner"}

	_, _, err := setupProcessCommand(processCobraCommand(t, RunCommand(), flags), args, "run")
	require.ErrorIs(t, err, scriptreview.ErrDeclined)
	require.Len(t, *reviewed, 1)
	require.Empty(t, gateway.requests())

	t.Setenv("RACK_GATEWAY_API_TOKEN", "rgw_test_token")
	_, _, err = setupProcessCommand(processCobraCommand(t, RunCommand(), flags), args, "run")
	require.NoError(t, err)
	require.Len(t, *reviewed, 1)
}

// A script needs the command that runs it and a justification, and must be a UTF-8 text file. Each is refused
// before the review.
func TestProcessCommandRefusesAScriptItCantSend(t *testing.T) {
	newEnvChangeGateway(t)
	reviewed := reviewedScripts(t, nil)
	script := writeScript(t, "count.rb", "puts 1\n")
	longWord := strings.Repeat("x", audit.MaxRecordedCommandBytes)
	for name, tc := range map[string]struct {
		script, justification string
		words                 []string
		want                  string
	}{
		"no command words":  {script, testJustification, nil, "--script needs the command that runs it"},
		"no justification":  {script, "", []string{"rails", "runner"}, "--script needs --justification"},
		"a missing file":    {script + ".missing", testJustification, []string{"cat"}, "no such file"},
		"a directory":       {filepath.Dir(script), testJustification, []string{"cat"}, "isn't a script file"},
		"an empty file":     {writeScript(t, "empty.rb", ""), testJustification, []string{"cat"}, "is empty"},
		"a binary file":     {writeScript(t, "bin", "a\x00b"), testJustification, []string{"cat"}, "NUL bytes"},
		"invalid UTF-8":     {writeScript(t, "latin1.rb", "Jos\xe9"), testJustification, []string{"cat"}, "UTF-8"},
		"too long to audit": {script, testJustification, []string{longWord}, "too long for the audit log"},
	} {
		flags := map[string]string{scriptFlag: tc.script}
		if tc.justification != "" {
			flags[justificationFlag] = tc.justification
		}
		cmd := processCobraCommand(t, ExecCommand(), flags)
		_, _, err := setupProcessCommand(cmd, append([]string{"p-1"}, tc.words...), "exec")
		require.ErrorContains(t, err, tc.want, name)
	}
	require.Empty(t, *reviewed)
}
