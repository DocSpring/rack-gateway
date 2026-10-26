package cli

import (
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
)

// The flags of exec and run that choose a command's env vars (see cmdenv.Request). An app's command_env_vars
// setting lists the env vars every command gets; without one, every command gets every env var except RACK_URL.
const (
	envFlag        = "env"
	allEnvVarsFlag = "all-env-vars"
	writeFlag      = "write"
)

// addEnvFlags adds --env, --all-env-vars and --write to the exec or run command cmd.
func addEnvFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice(envFlag, nil, "an env var the command needs besides the ones every command in the "+
		"app gets (repeat the flag, or separate names with commas)")
	cmd.Flags().Bool(allEnvVarsFlag, false, "give the command every env var of the app (except RACK_URL)")
	cmd.Flags().Bool(writeFlag, false, "give the command the real values of the app's read-only env vars (such "+
		"as a database URL), so it can change data")
}

// envRequest returns what the command's flags ask for, checked.
func envRequest(cmd *cobra.Command) (cmdenv.Request, error) {
	var req cmdenv.Request
	var err error
	if req.Env, err = cmd.Flags().GetStringSlice(envFlag); err != nil {
		return req, err
	}
	for i, name := range req.Env {
		req.Env[i] = strings.TrimSpace(name)
	}
	if req.AllEnvVars, err = cmd.Flags().GetBool(allEnvVarsFlag); err != nil {
		return req, err
	}
	if req.Write, err = cmd.Flags().GetBool(writeFlag); err != nil {
		return req, err
	}
	return req, req.Check()
}

// envPlan returns the env vars the command gets, as the gateway decides them from the app's settings, and whether
// the settings could be read (API tokens can't read them).
func (p processCommand) envPlan() (cmdenv.Plan, bool) {
	if !p.settings.known {
		return cmdenv.Plan{}, false
	}
	plan, err := p.settings.env.Plan(p.env)
	return plan, err == nil
}

// envSummary describes the env vars the command gets, for the PIN dialog, or "" when the app's settings couldn't
// be read.
func (p processCommand) envSummary() string {
	plan, ok := p.envPlan()
	if !ok {
		return ""
	}
	return "Env vars: " + plan.Describe()
}

// writtenVars are the app's read-only env vars the command gets the real values of (--write), sorted.
func (p processCommand) writtenVars() []string {
	plan, ok := p.envPlan()
	if !ok || !p.env.Write {
		return nil
	}
	var names []string
	for name := range p.settings.env.ReadOnly {
		if plan.All || slices.Contains(plan.Vars, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// canChangeData reports whether --write gives the command the real value of a read-only env var.
func (p processCommand) canChangeData() bool {
	return len(p.writtenVars()) > 0
}

// envWarnings say what the person approving the command must notice about its env vars: it can change data
// (--write), it gets every env var where the app gives commands only some (--all-env-vars), or it's an exec, which
// can read the environment of the process it runs in, whatever env vars it gets itself.
func (p processCommand) envWarnings() []string {
	var warnings []string
	if written := p.writtenVars(); len(written) > 0 {
		warnings = append(warnings, "This command can change data: --write gives it the real "+
			strings.Join(written, ", ")+", not the read-only one.")
	}
	if p.env.AllEnvVars && len(p.settings.env.Vars) > 0 {
		warnings = append(warnings, "--all-env-vars gives this command every env var of the app (except "+
			cmdenv.RackURL+"), credentials included.")
	}
	if p.action == "exec" && p.settings.env.Limits() {
		warnings = append(warnings, "exec runs inside a running process, so the command can read that process's "+
			"environment: every env var, with its real value.")
	}
	return warnings
}
