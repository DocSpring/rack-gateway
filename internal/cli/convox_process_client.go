package cli

import (
	"io"

	"github.com/convox/convox/pkg/structs"
	"github.com/convox/convox/sdk"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/DocSpring/rack-gateway/internal/cli/terminal"
	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/convox"
)

// processClient is the Convox SDK client exec and run use. The gateway refuses a command run without an MFA proof
// of its own, whatever the step-up window says, and a proof is single-use and short-lived (a TOTP code lasts a
// minute at most, a WebAuthn challenge five), while a run can wait minutes for its process to start. So the proof
// is collected when the command is about to run and goes with that request only: the exec, or a detached run's
// process start. `convox run`'s placeholder start and the requests around the exec (waiting for the process,
// stopping it) carry none; the exec's proof opens the step-up window that stopping the process needs.
type processClient struct {
	*sdk.Client
	// withProof returns a client whose requests carry a newly collected MFA proof.
	withProof func() (*sdk.Client, error)
}

// ProcessRun starts a process: `convox run`'s placeholder without a proof, any other command with one.
func (c *processClient) ProcessRun(app, service string, opts structs.ProcessRunOptions) (*structs.Process, error) {
	if opts.Command != nil && *opts.Command == convox.AttachedRunPlaceholder {
		return c.Client.ProcessRun(app, service, opts)
	}
	client, err := c.withProof()
	if err != nil {
		return nil, err
	}
	return client.ProcessRun(app, service, opts)
}

// ProcessExec runs command in a running process, with an MFA proof collected now.
func (c *processClient) ProcessExec(
	app, pid, command string, rw io.ReadWriter, opts structs.ProcessExecOptions,
) (int, error) {
	client, err := c.withProof()
	if err != nil {
		return 0, err
	}
	return client.ProcessExec(app, pid, command, rw, opts)
}

// newProcessClient returns the client for an exec or run command, whose requests all carry headers. prompt says
// what the command's MFA proof approves; pin is the security key PIN already entered for it, or "".
func newProcessClient(
	cobraCmd *cobra.Command, headers map[string]string, prompt webauthn.Prompt, pin string,
) (*processClient, error) {
	client, err := newHeaderClient("", headers)
	if err != nil {
		return nil, err
	}
	// The prompts run while Convox has the terminal in raw mode for the command's input, so they get back the state
	// it had before.
	before := stdinTerminalState()
	withProof := func() (*sdk.Client, error) {
		var mfaAuth string
		err := withTerminalState(before, func() error {
			var err error
			mfaAuth, err = freshMFAAuth(cobraCmd, prompt, pin)
			return err
		})
		if err != nil {
			return nil, err
		}
		return newHeaderClient(mfaAuth, headers)
	}
	return &processClient{Client: client, withProof: withProof}, nil
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

// stdinTerminalState returns stdin's terminal state, or nil when stdin isn't a terminal.
func stdinTerminalState() *term.State {
	fd, ok := terminal.StdinFD()
	if !ok {
		return nil
	}
	state, err := term.GetState(fd)
	if err != nil {
		return nil
	}
	return state
}

// withTerminalState runs fn with stdin's terminal in state (nil: as it is), then puts it back as it was.
func withTerminalState(state *term.State, fn func() error) error {
	fd, ok := terminal.StdinFD()
	if state == nil || !ok {
		return fn()
	}
	current, err := term.GetState(fd)
	if err != nil || term.Restore(fd, state) != nil {
		return fn()
	}
	defer func() { _ = term.Restore(fd, current) }()
	return fn()
}
