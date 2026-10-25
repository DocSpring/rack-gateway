package cli

import (
	"bytes"
	"io"
	"testing"

	"github.com/convox/convox/pkg/structs"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

const (
	authWithoutProof = "convox:SESSION-TOKEN-SECRET"
	authWithProof    = authWithoutProof + ".totp.123456"
)

// newTestProcessClient returns the process client of cmd (run or exec) for a fake gateway whose step-up window is
// open, with 123456 as --mfa-code.
func newTestProcessClient(t *testing.T, cmd *cobra.Command) (*processClient, *envChangeGateway) {
	t.Helper()
	gateway := newEnvChangeGateway(t)
	MFACodeFlag = "123456"
	cmd.SetErr(io.Discard)
	client, err := newProcessClient(cmd, nil, webauthn.Prompt{})
	require.NoError(t, err)
	require.Empty(t, gateway.requests(), "nothing is sent before the command runs")
	return client, gateway
}

// `convox run` starts its placeholder without a proof, even with the step-up window open, so the proof is left for
// the exec. A detached run's start runs the command itself and carries the proof. (The fake gateway answers every
// rack request with [], so the starts fail once their requests are recorded.)
func TestProcessClientSendsAProofOnlyWithTheCommand(t *testing.T) {
	client, gateway := newTestProcessClient(t, RunCommand())

	placeholder := convox.AttachedRunPlaceholder
	_, _ = client.ProcessRun("myapp", "web", structs.ProcessRunOptions{Command: &placeholder})
	command := "rails runner 'puts 1'"
	_, _ = client.ProcessRun("myapp", "web", structs.ProcessRunOptions{Command: &command})

	proxied := gateway.requests()
	require.Len(t, proxied, 2)
	require.Equal(t, authWithoutProof, basicCredential(t, proxied[0]))
	require.Equal(t, authWithProof, basicCredential(t, proxied[1]))
}

// The exec carries the command's proof, even with the step-up window open. (The fake gateway answers with JSON,
// so the exec fails once its requests are recorded: the SDK tries an HTTP exec, then the WebSocket handshake.)
func TestProcessClientSendsTheProofWithTheExec(t *testing.T) {
	client, gateway := newTestProcessClient(t, ExecCommand())

	_, _ = client.ProcessExec("myapp", "p-1", "rails runner 'puts 1'", &bytes.Buffer{}, structs.ProcessExecOptions{})

	proxied := gateway.requests()
	require.NotEmpty(t, proxied)
	for _, header := range proxied {
		require.Equal(t, authWithProof, basicCredential(t, header))
	}
}

const testJustification = "Count the templates of account 42 — checking a support ticket"

// processCommand returns cmd (run or exec) for app myapp, with flags set.
func processCommand(t *testing.T, cmd *cobra.Command, flags map[string]string) *cobra.Command {
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
		cmd = processCommand(t, cmd, nil)
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

	short := processCommand(t, RunCommand(), map[string]string{justificationFlag: "checking"})
	_, _, err := setupProcessCommand(short, args, "run")
	require.ErrorContains(t, err, "the justification is too short")

	cmd := processCommand(t, RunCommand(), map[string]string{justificationFlag: testJustification})
	client, _, err := setupProcessCommand(cmd, args, "run")
	require.NoError(t, err)
	placeholder := convox.AttachedRunPlaceholder
	_, _ = client.ProcessRun("myapp", "web", structs.ProcessRunOptions{Command: &placeholder})

	proxied := gateway.requests()
	require.Len(t, proxied, 1)
	require.Equal(t, audit.EncodeJustification(testJustification), proxied[0].Get(audit.JustificationHeader))
}

// The PIN dialog shows where the command runs, the command and its justification.
func TestCommandPromptDescribesTheCommand(t *testing.T) {
	prompt := commandPrompt("run", "myapp", "us", "web", []string{"rails", "runner", "'puts 1'"}, testJustification)
	require.Equal(t, []string{
		"On app myapp (rack us), run in a new web process: rails runner 'puts 1'",
		"Justification: " + testJustification,
	}, prompt.Summary)
	require.Equal(t, "run a command on app myapp (rack us)", prompt.Touch)

	prompt = commandPrompt("exec", "myapp", "us", "p-1", []string{"ls"}, "")
	require.Equal(t, []string{"On app myapp (rack us), exec in process p-1: ls"}, prompt.Summary)
}
