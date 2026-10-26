package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// approvedCommands is approved_deploy_commands listing commands, each with no env vars of its own.
func approvedCommands(commands ...string) []settings.ApprovedCommand {
	approved := make([]settings.ApprovedCommand, 0, len(commands))
	for _, command := range commands {
		approved = append(approved, settings.ApprovedCommand{Command: command})
	}
	return approved
}

var (
	readOnlyDatabase = map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}
	stripeVars       = []string{"DATABASE_URL", "PATH", "STRIPE_KEY"}
)

// limitCommandEnv gives app's commands only vars, and DATABASE_URL from READ_ONLY_DATABASE_URL unless they write.
func (f *protectedEnvFixture) limitCommandEnv(t *testing.T, vars ...string) {
	t.Helper()
	require.NoError(t, settings.NewService(f.database).SetAppSettings("app", map[string]interface{}{
		settings.KeyCommandEnvVars: vars, settings.KeyCommandEnvReadOnly: readOnlyDatabase,
	}, nil))
}

// forwarded reads the command the rack got back into its plan and the command it runs.
func forwarded(t *testing.T, header http.Header) (cmdenv.Plan, string) {
	t.Helper()
	require.Empty(t, header.Values(cmdenv.EnvHeader))
	require.Empty(t, header.Values(cmdenv.AllEnvVarsHeader))
	require.Empty(t, header.Values(cmdenv.WriteHeader))
	plan, command, ok := cmdenv.Parse(header.Get("Command"))
	require.True(t, ok, "the rack got the command unwrapped: %s", header.Get("Command"))
	return plan, command
}

// A command gets the app's env vars and the ones it asks for, with the read-only ones from their substitutes. The
// rack gets the command wrapped to run that way; the audit log records the command as sent, and the env vars.
func TestACommandRunsWithTheAppsEnvVarsAndTheOnesItAsksFor(t *testing.T) {
	f := newProtectedEnvFixture(t)
	f.limitCommandEnv(t, "PATH", "DATABASE_URL")
	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), map[string]string{
		"Command": "rake db:migrate", cmdenv.EnvHeader: "STRIPE_KEY",
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	starts := f.processStarts()
	require.Len(t, starts, 1)
	plan, command := forwarded(t, starts[0])
	require.Equal(t, "rake db:migrate", command)
	require.Equal(t, cmdenv.Plan{Vars: stripeVars, ReadOnly: readOnlyDatabase}, plan)

	details := f.processStartDetails(t, "success")
	require.Equal(t, "rake db:migrate", details["command"])
	require.Equal(t, []interface{}{"DATABASE_URL", "PATH", "STRIPE_KEY"}, details["env_vars"])
	require.Equal(t, []interface{}{"STRIPE_KEY"}, details["env_vars_added"])
	require.Equal(t, map[string]interface{}{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}, details["env_read_only"])
	require.NotContains(t, details, "write")
	require.NotContains(t, details, "all_env_vars")
}

// --write gives a command the real values of the read-only vars, and the audit log says so.
func TestAWriteCommandGetsTheRealValues(t *testing.T) {
	f := newProtectedEnvFixture(t)
	f.limitCommandEnv(t, "PATH", "DATABASE_URL")
	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), map[string]string{
		"Command": "rake db:migrate", cmdenv.WriteHeader: "true",
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	plan, _ := forwarded(t, f.processStarts()[0])
	require.Equal(t, cmdenv.Plan{Vars: []string{"DATABASE_URL", "PATH"}}, plan)
	details := f.processStartDetails(t, "success")
	require.Equal(t, true, details["write"])
	require.NotContains(t, details, "env_read_only")
}

// An app that doesn't limit its commands' env vars gives each one every env var except RACK_URL, which no command
// gets.
func TestACommandInAnAppWithoutLimitsGetsEveryEnvVarButRackURL(t *testing.T) {
	f := newProtectedEnvFixture(t)
	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t),
		map[string]string{"Command": "rake db:migrate"})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	plan, command := forwarded(t, f.processStarts()[0])
	require.Equal(t, cmdenv.Plan{All: true}, plan)
	require.Equal(t, "rake db:migrate", command)
	require.Equal(t, true, f.processStartDetails(t, "success")["all_env_vars"])
}

// `convox run`'s placeholder gets no env vars at all, whatever it asks for: it needs no MFA proof, and a command
// exec'd into its container could read its environment.
func TestTheRunPlaceholderGetsNoEnvVars(t *testing.T) {
	f := newProtectedEnvFixture(t)
	headers := placeholderStart("")
	headers[cmdenv.AllEnvVarsHeader] = "true"
	headers[cmdenv.WriteHeader] = "true"
	rr := f.processRequest(t, http.MethodPost, processStartPath, "", headers)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	plan, command := forwarded(t, f.processStarts()[0])
	require.Equal(t, cmdenv.Plan{}, plan)
	require.Equal(t, convox.AttachedRunPlaceholder, command)
	details := f.processStartDetails(t, "success")
	require.Equal(t, []interface{}{}, details["env_vars"])
	require.NotContains(t, details, "write")
}

// A command the gateway can't wrap, or one asking for env vars it can't have, is refused before it reaches the rack,
// and the refusal is audited.
func TestCommandsTheGatewayCantWrapAreRefused(t *testing.T) {
	for _, c := range []struct {
		reason  string
		headers map[string]string
	}{
		{"no command can have RACK_URL", map[string]string{"Command": "rake", cmdenv.EnvHeader: "RACK_URL"}},
		{"isn't an env var name", map[string]string{"Command": "rake", cmdenv.EnvHeader: "A B"}},
		{"X-Command-Write must be true", map[string]string{"Command": "rake", cmdenv.WriteHeader: "1"}},
		{
			"can't name some with --env too",
			map[string]string{"Command": "rake", cmdenv.EnvHeader: "A", cmdenv.AllEnvVarsHeader: "true"},
		},
		{"the command is empty", map[string]string{"Command": "  "}},
		{"the command is empty", map[string]string{}},
		{"can't start with NAME=value", map[string]string{"Command": "DATABASE_URL=postgres://x rake db:migrate"}},
		{"can't split the command", map[string]string{"Command": "rake 'unbalanced"}},
	} {
		t.Run(c.reason, func(t *testing.T) {
			f := newProtectedEnvFixture(t)
			rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), c.headers)
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
			require.Contains(t, rr.Body.String(), c.reason)
			require.Empty(t, f.processStarts())
			require.Contains(t, f.processStartDetails(t, "denied")["reason"], c.reason)
		})
	}
}

// A command in an app whose command settings can't be read is refused: running it with every env var would give it
// what the settings keep from it.
func TestACommandIsRefusedWhenTheAppsCommandSettingsCantBeRead(t *testing.T) {
	f := newProtectedEnvFixture(t)
	app := "app"
	require.NoError(t, f.database.UpsertSetting(&app, settings.KeyCommandEnvVars, "PATH", nil))
	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), map[string]string{"Command": "rake"})
	require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
	require.Empty(t, f.processStarts())
}

// An exec is wrapped the same way, and its audit entries record its env vars.
func TestAnExecRunsWithTheAppsEnvVars(t *testing.T) {
	f := newProtectedEnvFixture(t)
	f.limitCommandEnv(t, "PATH", "DATABASE_URL")
	envReq, _ := f.request(t, f.admin, "", "", f.totpCode(t), stepUpAgeFresh)
	authUser, ok := auth.GetAuthUser(envReq.Context())
	require.True(t, ok)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.h.ProxyToRack(w, r.WithContext(context.WithValue(r.Context(), auth.UserContextKey, authUser)))
	}))
	t.Cleanup(server.Close)

	header := http.Header{"Command": {"bin/rails console"}, cmdenv.EnvHeader: {"STRIPE_KEY"}}
	conn, resp, err := websocket.DefaultDialer.Dial(websocketURL(server.URL)+"/apps/app/processes/p-1/exec", header)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	// The rack ends the session at once; once the client's connection closes, the exec is on record.
	_, _, err = conn.ReadMessage()
	require.Error(t, err)
	_ = conn.Close()

	f.rack.mu.Lock()
	execs := append([]http.Header(nil), f.rack.execs...)
	f.rack.mu.Unlock()
	require.Len(t, execs, 1)
	plan, command := forwarded(t, execs[0])
	require.Equal(t, "bin/rails console", command)
	require.Equal(t, cmdenv.Plan{Vars: stripeVars, ReadOnly: readOnlyDatabase}, plan)
	details := f.processStartDetails(t, "success")
	require.Equal(t, "bin/rails console", details["command"])
	require.Equal(t, []interface{}{"DATABASE_URL", "PATH", "STRIPE_KEY"}, details["env_vars"])
}

// approvedCommandRequest is a request running command in docspring under the harness's approval, with headers.
func (e *approvalExecHarness) approvedCommandRequest(command string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/apps/docspring/processes/p1/exec", nil)
	r.Header.Set("Command", command)
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	tracker := &deployApprovalTracker{request: e.approval, app: "docspring"}
	return r.WithContext(context.WithValue(r.Context(), deployApprovalContextKey, tracker))
}

// Under a deploy approval a command's entry in approved_deploy_commands decides its env vars, and a command that
// asks for any itself is refused. A command that isn't approved is left undecided, so it's never forwarded.
func TestAnApprovedCommandsEntryDecidesItsEnvVars(t *testing.T) {
	e := newApprovalExecHarness(t)
	app := "docspring"
	require.NoError(t, e.database.UpsertSetting(&app, settings.KeyCommandEnvVars, []string{"PATH"}, nil))
	require.NoError(t, e.database.UpsertSetting(&app, settings.KeyCommandEnvReadOnly, readOnlyDatabase, nil))
	require.NoError(t, e.database.UpsertSetting(&app, settings.KeyApprovedDeployCommands, []settings.ApprovedCommand{
		{Command: "bin/pre_release_checks migrate", Env: []string{"DATABASE_URL"}, Write: true},
		{Command: "rake stripe:prepare", Env: []string{"DATABASE_URL", "STRIPE_KEY"}},
		{Command: "bin/pre_release_checks", AllEnvVars: true},
	}, nil))

	for command, want := range map[string]audit.CommandEnv{
		"bin/pre_release_checks migrate": {
			Plan: cmdenv.Plan{Vars: []string{"DATABASE_URL", "PATH"}, Added: []string{"DATABASE_URL"}}, Write: true,
		},
		"rake stripe:prepare": {Plan: cmdenv.Plan{
			Vars: []string{"DATABASE_URL", "PATH", "STRIPE_KEY"}, ReadOnly: readOnlyDatabase,
			Added: []string{"DATABASE_URL", "STRIPE_KEY"},
		}},
		"bin/pre_release_checks": {Plan: cmdenv.Plan{All: true, ReadOnly: readOnlyDatabase}},
	} {
		r := e.approvedCommandRequest(command, nil)
		require.NoError(t, e.h.checkCommandEnv(r, getDeployApprovalTracker(r.Context())), command)
		env, decided := audit.CommandEnvFromContext(r.Context())
		require.True(t, decided, command)
		require.Equal(t, want, env, command)
	}

	r := e.approvedCommandRequest("bin/pre_release_checks migrate", map[string]string{cmdenv.WriteHeader: "true"})
	err := e.h.checkCommandEnv(r, getDeployApprovalTracker(r.Context()))
	require.ErrorIs(t, err, errApprovedCommandEnv)

	r = e.approvedCommandRequest("bin/rails console", nil)
	require.NoError(t, e.h.checkCommandEnv(r, getDeployApprovalTracker(r.Context())))
	_, decided := audit.CommandEnvFromContext(r.Context())
	require.False(t, decided)
	require.ErrorIs(t, wrapCommand(r, http.Header{}), errEnvUndecided)
}
