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
