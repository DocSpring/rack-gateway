package cli

import (
	"fmt"
	"net/textproto"
	"os"
	"strings"

	"github.com/convox/stdcli"
	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/scriptreview"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// scriptFlag names a local script file to run with the command (see scriptrun).
const scriptFlag = "script"

// maxScriptFileBytes caps the script file read: the audit log keeps at most 50 KB of command and script anyway.
const maxScriptFileBytes = 1 << 20

// addProcessCommandFlags adds the flags exec and run share: --justification and --script.
func addProcessCommandFlags(cmd *cobra.Command) {
	addJustificationFlag(cmd)
	cmd.Flags().String(scriptFlag, "", "a local script file to run: the command gets the file's path as its last "+
		"argument (e.g. rails runner), and you review the script before it runs. Needs --justification")
}

// processCommand is an exec or run command as it's sent: where, the command, its justification and its script.
type processCommand struct {
	action, app, rack string
	// target is the process (exec) or service (run).
	target string
	// command is the command as sent (the Command header).
	command       string
	justification string
	// script is the --script file, as the gateway will record it (nil without one).
	script *scriptrun.Script
}

// where names the command's app and rack.
func (p processCommand) where() string {
	return fmt.Sprintf("app %s (rack %s)", displayText(p.app), displayText(p.rack))
}

// shownCommand is the command as a person would type it: a script's words and file name, not its base64.
func (p processCommand) shownCommand() string {
	if p.script != nil {
		return p.script.Command()
	}
	return p.command
}

// headers are the gateway headers every request of the command carries.
func (p processCommand) headers() map[string]string {
	headers := map[string]string{}
	if p.justification != "" {
		headers[audit.JustificationHeader] = audit.EncodeJustification(p.justification)
	}
	return headers
}

// setupProcessCommand prepares exec and run, whose first argument is the process (exec) or service (run) and the
// rest the command to run (Convox joins them with spaces). The gateway refuses a command too long for its audit
// log, or without a justification in an app that requires one, but the Convox SDK drops the answer to a refused
// WebSocket handshake, so an exec it refused would only show "websocket: bad handshake". Checking the command here
// explains the refusal, before any MFA prompt or process start. A --script is reviewed here too.
func setupProcessCommand(
	cobraCmd *cobra.Command, args []string, action string,
) (*processClient, *stdcli.Context, error) {
	if err := resolveAppFlag(cobraCmd); err != nil {
		return nil, nil, err
	}
	process, err := newProcessCommand(cobraCmd, args, action)
	if err != nil {
		return nil, nil, err
	}
	if err := reviewScript(process); err != nil {
		return nil, nil, err
	}
	client, err := newProcessClient(cobraCmd, process.headers(), commandPrompt(process))
	if err != nil {
		return nil, nil, err
	}
	if process.script != nil {
		args = []string{args[0], process.command}
	}
	return client, newConvoxContext(cobraCmd, args), nil
}

// newProcessCommand checks the command and its flags, and reads its script.
func newProcessCommand(cobraCmd *cobra.Command, args []string, action string) (processCommand, error) {
	process := processCommand{action: action, target: args[0], command: sentCommand(args[1:])}
	scriptPath, err := cobraCmd.Flags().GetString(scriptFlag)
	if err != nil {
		return process, err
	}
	if scriptPath != "" {
		if err := process.addScript(scriptPath); err != nil {
			return process, err
		}
	}
	if err := audit.CheckCommandLength(process.command); err != nil {
		return process, err
	}
	if process.app, err = cobraCmd.Flags().GetString("app"); err != nil {
		return process, err
	}
	if process.justification, err = commandJustification(cobraCmd, process.app); err != nil {
		return process, err
	}
	if process.script != nil && process.justification == "" {
		return process, fmt.Errorf("--%s needs --%s: say why the script is run and what it does",
			scriptFlag, justificationFlag)
	}
	process.rack, err = SelectedRack()
	return process, err
}

// addScript reads the script file at path and makes the command send it with the command words.
func (p *processCommand) addScript(path string) error {
	if strings.TrimSpace(p.command) == "" {
		return fmt.Errorf("--%s needs the command that runs it after the %s, e.g. rails runner", scriptFlag,
			map[string]string{"exec": "process ID", "run": "service"}[p.action])
	}
	text, err := readScriptFile(path)
	if err != nil {
		return err
	}
	p.command = scriptrun.Build(scriptrun.SafeName(path), text, p.command)
	script, ok := scriptrun.Parse(p.command)
	if !ok {
		return fmt.Errorf("%s can't be sent as a script", displayText(path))
	}
	p.script = &script
	return nil
}

// readScriptFile reads a script file: UTF-8 text, not empty, without NUL bytes, at most maxScriptFileBytes.
func readScriptFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxScriptFileBytes {
		return nil, fmt.Errorf("%s isn't a script file of at most %d KB", displayText(path), maxScriptFileBytes>>10)
	}
	text, err := os.ReadFile(path) // #nosec G304 -- the user names the file to send
	if err != nil {
		return nil, err
	}
	if err := scriptrun.Check(text); err != nil {
		return nil, fmt.Errorf("%s: %w", displayText(path), err)
	}
	return text, nil
}

// reviewScript has the person running the command approve its script before anything is sent. API tokens run
// unattended: there's no one to ask.
func reviewScript(process processCommand) error {
	if process.script == nil || apiTokenOverride() != "" {
		return nil
	}
	return reviewScriptWith(process)
}

// reviewScriptWith shows a script for approval (scriptreview.Review). Tests replace it.
var reviewScriptWith = func(process processCommand) error {
	return scriptreview.Review(scriptreview.Request{
		Where: process.where(), Justification: process.justification, Script: *process.script,
	})
}

// headerLineBreaks are what Go's HTTP client replaces with spaces in a header value.
var headerLineBreaks = strings.NewReplacer("\n", " ", "\r", " ")

// sentCommand is the command words as the gateway receives them in the Command header: joined with spaces, line
// breaks made spaces and the ends trimmed, as Go's HTTP client writes a header value.
func sentCommand(words []string) string {
	return textproto.TrimString(headerLineBreaks.Replace(strings.Join(words, " ")))
}
