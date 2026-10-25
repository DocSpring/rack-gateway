package cli

import (
	"io"

	"github.com/convox/convox/pkg/structs"
	"github.com/convox/convox/sdk"
	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
)

// processClient is the Convox SDK client exec and run use. The gateway refuses a command run without an MFA proof
// of its own, whatever the step-up window says, and a proof is single-use. The proof is collected before anything
// is sent, so the key touch follows the PIN at once, and goes with the one request that runs the command: the exec,
// or a detached run's process start. `convox run`'s placeholder start and the requests around the exec (waiting for
// the process, stopping it) carry none; the exec's proof opens the step-up window that stopping the process needs.
// A process takes seconds to start, and a WebAuthn challenge lasts five minutes (a TOTP code at least 30 seconds).
type processClient struct {
	*sdk.Client
	// proof is the client whose requests carry the command's MFA proof.
	proof *sdk.Client
}

// ProcessRun starts a process: `convox run`'s placeholder without a proof, any other command with one.
func (c *processClient) ProcessRun(app, service string, opts structs.ProcessRunOptions) (*structs.Process, error) {
	if opts.Command != nil && *opts.Command == convox.AttachedRunPlaceholder {
		return c.Client.ProcessRun(app, service, opts)
	}
	return c.proof.ProcessRun(app, service, opts)
}

// ProcessExec runs command in a running process, with the command's MFA proof.
func (c *processClient) ProcessExec(
	app, pid, command string, rw io.ReadWriter, opts structs.ProcessExecOptions,
) (int, error) {
	return c.proof.ProcessExec(app, pid, command, rw, opts)
}

// newProcessClient collects the MFA proof of an exec or run command and returns the command's client, whose
// requests all carry headers. prompt says what the proof approves; pin is the security key PIN already entered for
// it, or "".
func newProcessClient(
	cobraCmd *cobra.Command, headers map[string]string, prompt webauthn.Prompt, pin string,
) (*processClient, error) {
	client, err := newHeaderClient("", headers)
	if err != nil {
		return nil, err
	}
	mfaAuth, err := freshMFAAuth(cobraCmd, prompt, pin)
	if err != nil {
		return nil, err
	}
	proof, err := newHeaderClient(mfaAuth, headers)
	if err != nil {
		return nil, err
	}
	return &processClient{Client: client, proof: proof}, nil
}

// newHeaderClient returns a client carrying mfaAuth (see newConvoxClient) that adds headers to every request.
func newHeaderClient(mfaAuth string, headers map[string]string) (*sdk.Client, error) {
	client, err := newConvoxClient(mfaAuth)
	if err != nil {
		return nil, err
	}
	if len(headers) > 0 {
		if client.ExtraHeaders == nil {
			client.ExtraHeaders = map[string]string{}
		}
		for name, value := range headers {
			client.ExtraHeaders[name] = value
		}
	}
	return client, nil
}
