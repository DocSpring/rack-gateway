package cli

import (
	"bytes"
	"testing"

	"github.com/convox/convox/pkg/structs"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// limitedEnv is an app whose commands get PATH and DATABASE_URL, which is read-only.
const limitedEnv = `{"command_env_vars":{"value":["PATH","DATABASE_URL"]},` +
	`"command_env_read_only":{"value":{"DATABASE_URL":"READ_ONLY_DATABASE_URL"}}}`

// --env, --all-env-vars and --write go with each of the command's requests, as the headers the gateway reads.
func TestProcessCommandSendsWhatItAsksFor(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	cmd := processCobraCommand(t, RunCommand(), map[string]string{envFlag: "STRIPE_KEY", writeFlag: "true"})
	require.NoError(t, cmd.Flags().Set(envFlag, " SENTRY_DSN,http_proxy"))
	client, _, err := setupProcessCommand(t, cmd, []string{"web", "rake", "stripe:prepare"}, "run")
	require.NoError(t, err)
	placeholder := convox.AttachedRunPlaceholder
	_, _ = client.ProcessRun("myapp", "web", structs.ProcessRunOptions{Command: &placeholder})

	proxied := gateway.requests()
	require.Len(t, proxied, 1)
	require.Equal(t, "STRIPE_KEY,SENTRY_DSN,http_proxy", proxied[0].Get(cmdenv.EnvHeader))
	require.Equal(t, "true", proxied[0].Get(cmdenv.WriteHeader))
	require.Empty(t, proxied[0].Get(cmdenv.AllEnvVarsHeader))
}

// A command the gateway would refuse is refused before anything is sent: env vars it can't have, a command it
// can't wrap.
func TestProcessCommandRefusesWhatTheGatewayWould(t *testing.T) {
	for want, c := range map[string]struct {
		flags map[string]string
		args  []string
	}{
		"no command can have RACK_URL": {map[string]string{envFlag: "RACK_URL"}, []string{"web", "env"}},
		"isn't an env var name":        {map[string]string{envFlag: "A-B"}, []string{"web", "env"}},
		"can't name some with --env too": {
			map[string]string{envFlag: "A", allEnvVarsFlag: "true"}, []string{"web", "env"},
		},
		"can't start with NAME=value": {nil, []string{"web", "DATABASE_URL=postgres://x", "rake"}},
		"the command is empty":        {nil, []string{"web"}},
	} {
		gateway := newEnvChangeGateway(t)
		for _, cmd := range []*cobra.Command{ExecCommand(), RunCommand()} {
			cmd = processCobraCommand(t, cmd, c.flags)
			_, _, err := setupProcessCommand(t, cmd, c.args, cmd.Name())
			require.ErrorContains(t, err, want)
		}
		require.Empty(t, gateway.requests(), want)
	}
}

// limitedProcess is a command in an app whose commands get PATH and DATABASE_URL, which is read-only.
func limitedProcess(action string, req cmdenv.Request) processCommand {
	return processCommand{
		action: action, app: "myapp", rack: "us", target: "web", command: "rake db:migrate", env: req,
		settings: commandSettings{known: true, env: settings.CommandEnv{
			Vars:     []string{"PATH", "DATABASE_URL"},
			ReadOnly: map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"},
		}},
	}
}

// The PIN dialog says which env vars the command gets, and warns in red when it can change data (--write), gets
// every env var where the app gives commands only some (--all-env-vars), or is an exec, which can read its process's
// environment. The touch says when it can change data.
func TestCommandPromptDescribesTheCommandsEnvVars(t *testing.T) {
	prompt := commandPrompt(limitedProcess("run", cmdenv.Request{Env: []string{"STRIPE_KEY"}}))
	require.Contains(t, prompt.Summary, "Env vars: DATABASE_URL (from READ_ONLY_DATABASE_URL), PATH, STRIPE_KEY")
	require.Empty(t, prompt.Warnings)
	require.Equal(t, "run a command on app myapp (rack us)", prompt.Touch)

	prompt = commandPrompt(limitedProcess("run", cmdenv.Request{Write: true}))
	require.Contains(t, prompt.Summary, "Env vars: DATABASE_URL, PATH")
	require.Equal(t, []string{
		"This command can change data: --write gives it the real DATABASE_URL, not the read-only one.",
	}, prompt.Warnings)
	require.Equal(t, "run a command that can change data on app myapp (rack us)", prompt.Touch)

	prompt = commandPrompt(limitedProcess("run", cmdenv.Request{AllEnvVars: true}))
	require.Contains(t, prompt.Summary, "Env vars: every env var except RACK_URL; read-only DATABASE_URL "+
		"(from READ_ONLY_DATABASE_URL)")
	require.Len(t, prompt.Warnings, 1)
	require.Contains(t, prompt.Warnings[0], "--all-env-vars gives this command every env var of the app")

	prompt = commandPrompt(limitedProcess("exec", cmdenv.Request{}))
	require.Len(t, prompt.Warnings, 1)
	require.Contains(t, prompt.Warnings[0], "exec runs inside a running process")

	// An app that keeps nothing from its commands has nothing to warn about: --write gives no command more.
	unlimited := limitedProcess("exec", cmdenv.Request{Write: true, AllEnvVars: true})
	unlimited.settings.env = settings.CommandEnv{}
	prompt = commandPrompt(unlimited)
	require.Contains(t, prompt.Summary, "Env vars: every env var except RACK_URL")
	require.Empty(t, prompt.Warnings)

	// An API token can't read settings, and has no PIN dialog.
	token := limitedProcess("run", cmdenv.Request{Write: true})
	token.settings = commandSettings{}
	prompt = commandPrompt(token)
	require.NotContains(t, prompt.Summary, "Env vars")
	require.Empty(t, prompt.Warnings)
}

// The CLI reads the app's command settings from the gateway, and a dry run shows the env vars and the warnings.
func TestProcessCommandDryRunShowsTheEnvVars(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	gateway.appSettings = limitedEnv
	cmd := processCobraCommand(t, RunCommand(), map[string]string{writeFlag: "true", dryRunFlag: "true"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runProcessCommand(cmd, []string{"web", "rake", "db:migrate"}, "run", nil))
	require.Contains(t, out.String(), "Env vars: DATABASE_URL, PATH")
	require.Contains(t, out.String(), "WARNING: This command can change data: --write gives it the real DATABASE_URL")
	require.Empty(t, gateway.requests())
}
