package cli

import (
	"fmt"
	"io"
	"net/textproto"
	"os"
	"strings"

	"github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// scriptFlag names a local script file to run with the command (see scriptrun).
const scriptFlag = "script"

// dryRunFlag stops a command once it's checked and its script reviewed: nothing is sent to the rack.
const dryRunFlag = "dry-run"

// maxScriptFileBytes caps the script file read: the audit log keeps at most 50 KB of command and script anyway.
const maxScriptFileBytes = 1 << 20

// addProcessCommandFlags adds the flags exec and run share: --justification, --script, --dry-run and the env var
// flags (--env, --all-env-vars and --write).
func addProcessCommandFlags(cmd *cobra.Command) {
	addJustificationFlag(cmd)
	addEnvFlags(cmd)
	cmd.Flags().String(scriptFlag, "", "a local script file to run: the command gets the file's path as its last "+
		"argument (e.g. rails runner), and you review the script before it runs. Needs --justification")
	cmd.Flags().Bool(dryRunFlag, false, "check the command and review its script, then show what would be sent, "+
		"without asking for MFA or sending anything to the rack")
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
	// env is what the command asks for: env vars besides the app's, every env var, or write access.
	env cmdenv.Request
	// settings are the app's settings for commands.
	settings commandSettings
	// dryRun stops the command before anything is sent (--dry-run).
	dryRun bool
	// pin is the security key PIN entered in the script review dialog, for the command's MFA proof ("" when the
	// PIN is asked for when the command runs). It's only ever passed to the security key.
	pin string
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
	for name, value := range p.env.Headers() {
		headers[name] = value
	}
	return headers
}

// runProcessCommand runs exec or run with run (cli.Exec or cli.Run). Their first argument is the process (exec)
// or service (run) and the rest the command to run (Convox joins them with spaces). With --dry-run it stops once
// the command is checked and its script reviewed, and prints what would be sent.
func runProcessCommand(
	cobraCmd *cobra.Command, args []string, action string, run func(sdk.Interface, *stdcli.Context) error,
) error {
	process, err := prepareProcessCommand(cobraCmd, args, action)
	if err != nil {
		return err
	}
	if process.dryRun {
		return printDryRun(cobraCmd.OutOrStdout(), process)
	}
	client, ctx, err := process.connect(cobraCmd, args)
	if err != nil {
		return err
	}
	return normalizeConvoxExit(run(client, ctx))
}

// prepareProcessCommand checks an exec or run command and reviews its script. The gateway refuses a command too
// long for its audit log, or without a justification in an app that requires one; checking the command here
// refuses it before any MFA prompt or process start.
func prepareProcessCommand(cobraCmd *cobra.Command, args []string, action string) (processCommand, error) {
	if err := resolveAppFlag(cobraCmd); err != nil {
		return processCommand{}, err
	}
	process, err := newProcessCommand(cobraCmd, args, action)
	if err != nil {
		return process, err
	}
	if process.dryRun, err = cobraCmd.Flags().GetBool(dryRunFlag); err != nil {
		return process, err
	}
	process.pin, err = reviewScript(process)
	return process, err
}

// connect returns the client and Convox context that run the command, args being the command line's arguments.
// A script's command replaces the command words.
func (p processCommand) connect(cobraCmd *cobra.Command, args []string) (*processClient, *stdcli.Context, error) {
	client, err := newProcessClient(cobraCmd, p.headers(), commandPrompt(p), p.pin)
	if err != nil {
		return nil, nil, err
	}
	if p.script != nil {
		args = []string{args[0], p.command}
	}
	return client, newConvoxContext(cobraCmd, args), nil
}

// printDryRun says what the command would send: what the PIN dialog would show, and the command's size.
func printDryRun(out io.Writer, process processCommand) error {
	prompt := commandPrompt(process)
	lines := append([]string{"Dry run: nothing was sent to the rack."}, prompt.Summary...)
	for _, warning := range prompt.Warnings {
		lines = append(lines, "WARNING: "+warning)
	}
	size := fmt.Sprintf("The command sent would be %d bytes", len(process.command))
	if process.script != nil {
		size += " (the script travels base64-encoded, with a sh runner that writes it to a temporary file)"
	}
	_, err := fmt.Fprintln(out, strings.Join(append(lines, size+"."), "\n"))
	return err
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
	if err := cmdenv.CheckCommand(process.command); err != nil {
		return process, err
	}
	if process.env, err = envRequest(cobraCmd); err != nil {
		return process, err
	}
	if process.app, err = cobraCmd.Flags().GetString("app"); err != nil {
		return process, err
	}
	if process.settings, err = readCommandSettings(cobraCmd, process.app); err != nil {
		return process, err
	}
	process.justification, err = commandJustification(cobraCmd, process.app, process.settings.justificationRequired)
	if err != nil {
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

// headerLineBreaks are what Go's HTTP client replaces with spaces in a header value.
var headerLineBreaks = strings.NewReplacer("\n", " ", "\r", " ")

// sentCommand is the command words as the gateway receives them in the Command header: joined with spaces, line
// breaks made spaces and the ends trimmed, as Go's HTTP client writes a header value.
func sentCommand(words []string) string {
	return textproto.TrimString(headerLineBreaks.Replace(strings.Join(words, " ")))
}
