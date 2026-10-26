package settings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestValidateCommandEnvVars(t *testing.T) {
	web := "web"
	for _, value := range []interface{}{
		[]interface{}{"PATH", " HOME ", "http_proxy"},
		[]string{},
	} {
		require.NoErrorf(t, ValidateSettingValue(&web, KeyCommandEnvVars, value), "%#v", value)
	}
	for _, value := range []interface{}{
		[]interface{}{"PATH", "RACK_URL"},
		[]interface{}{"BAD-NAME"},
		[]interface{}{""},
		[]interface{}{"PATH", 1},
		"PATH",
		nil,
	} {
		require.ErrorIsf(t, ValidateSettingValue(&web, KeyCommandEnvVars, value), ErrInvalidSettingValue, "%#v", value)
	}
}

func TestValidateCommandEnvReadOnly(t *testing.T) {
	web := "web"
	valid := map[string]interface{}{
		"DATABASE_URL": "READ_ONLY_DATABASE_URL", "DATABASE_URL_DIRECT": "READ_ONLY_DATABASE_URL",
	}
	require.NoError(t, ValidateSettingValue(&web, KeyCommandEnvReadOnly, valid))
	require.NoError(t, ValidateSettingValue(&web, KeyCommandEnvReadOnly, map[string]interface{}{}))
	for _, value := range []interface{}{
		map[string]interface{}{"RACK_URL": "X"},
		map[string]interface{}{"X": "RACK_URL"},
		map[string]interface{}{"A": "B", "B": "C"},
		map[string]interface{}{"A B": "C"},
		map[string]interface{}{"A": 1},
		[]interface{}{"DATABASE_URL"},
		nil,
	} {
		err := ValidateSettingValue(&web, KeyCommandEnvReadOnly, value)
		require.ErrorIsf(t, err, ErrInvalidSettingValue, "%#v", value)
	}
}

func TestValidateApprovedDeployCommands(t *testing.T) {
	web := "web"
	for _, value := range []interface{}{
		[]interface{}{},
		[]interface{}{map[string]interface{}{"command": "bin/pre_release_checks migrate", "write": true}},
		[]interface{}{
			map[string]interface{}{"command": "rake stripe:prepare", "env": []interface{}{"STRIPE_SECRET_KEY"}},
			map[string]interface{}{"command": "bin/pre_release_checks", "all_env_vars": true, "write": false},
		},
		[]ApprovedCommand{{Command: "bin/migrate", Write: true}},
	} {
		require.NoErrorf(t, ValidateSettingValue(&web, KeyApprovedDeployCommands, value), "%#v", value)
	}
	for want, value := range map[string]interface{}{
		"must be a JSON array of objects": "bin/migrate",
		"each entry must be an object":    []interface{}{"bin/migrate"},
		`unknown field "cmd"`:             []interface{}{map[string]interface{}{"cmd": "bin/migrate"}},
		"env":                             []interface{}{map[string]interface{}{"command": "x", "env": "A"}},
		"write":                           []interface{}{map[string]interface{}{"command": "x", "write": "yes"}},
		"entry 1: the command is empty":   []interface{}{map[string]interface{}{"command": " "}},
		"can't start with NAME=value":     []interface{}{map[string]interface{}{"command": "A=1 rake"}},
		"listed more than once": []interface{}{
			map[string]interface{}{"command": "bin/migrate"}, map[string]interface{}{"command": " bin/migrate"},
		},
		"no command can have RACK_URL": []interface{}{
			map[string]interface{}{"command": "x", "env": []interface{}{"RACK_URL"}},
		},
		"can't have env as well": []interface{}{
			map[string]interface{}{"command": "x", "env": []interface{}{"A"}, "all_env_vars": true},
		},
	} {
		err := ValidateSettingValue(&web, KeyApprovedDeployCommands, value)
		require.ErrorIsf(t, err, ErrInvalidSettingValue, "%#v", value)
		require.ErrorContainsf(t, err, want, "%#v", value)
	}
	require.ErrorIs(t, ValidateSettingValue(&web, KeyApprovedDeployCommands, nil), ErrInvalidSettingValue)
}

// The deployment config sets the command settings like any other: names as a list, the substitutes and the
// approved commands as JSON.
func TestCommandSettingsFromTheEnvironment(t *testing.T) {
	svc := NewService(dbtest.NewDatabase(t))
	t.Setenv("RGW_APP_WEB_SETTING_COMMAND_ENV_VARS", "PATH, HOME")
	t.Setenv("RGW_APP_WEB_SETTING_COMMAND_ENV_READ_ONLY", `{"DATABASE_URL":"READ_ONLY_DATABASE_URL"}`)
	t.Setenv("RGW_APP_WEB_SETTING_APPROVED_DEPLOY_COMMANDS",
		`[{"command":"bin/migrate","write":true},{"command":"rake stripe:prepare","env":["STRIPE_KEY"]}]`)

	env, err := svc.GetCommandEnv("web")
	require.NoError(t, err)
	require.Equal(t, CommandEnv{
		Vars: []string{"PATH", "HOME"}, ReadOnly: map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"},
	}, env)
	commands, err := svc.GetApprovedDeployCommands("web")
	require.NoError(t, err)
	require.Equal(t, []ApprovedCommand{
		{Command: "bin/migrate", Write: true}, {Command: "rake stripe:prepare", Env: []string{"STRIPE_KEY"}},
	}, commands)

	require.NoError(t, ValidateEnvironment([]string{
		"RGW_APP_WEB_SETTING_APPROVED_DEPLOY_COMMANDS=[{\"command\":\"bin/migrate\"}]",
	}))
	// The list of strings older releases took is refused, so the gateway won't start with it.
	require.ErrorContains(t, ValidateEnvironment([]string{
		`RGW_APP_WEB_SETTING_APPROVED_DEPLOY_COMMANDS=["bin/migrate"]`,
	}), "each entry must be an object")
	require.ErrorContains(t, ValidateEnvironment([]string{"RGW_APP_WEB_SETTING_COMMAND_ENV_VARS=PATH,RACK_URL"}),
		"no command can have RACK_URL")
}

// Unset, the settings let every command have every env var, as before they existed. A stored value of the wrong
// shape is an error, which refuses the app's commands.
func TestGetCommandEnv(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewService(database)
	env, err := svc.GetCommandEnv("web")
	require.NoError(t, err)
	require.Equal(t, CommandEnv{Vars: []string{}, ReadOnly: map[string]string{}}, env)

	app := "web"
	require.NoError(t, database.UpsertSetting(&app, KeyCommandEnvVars, []string{"PATH", "RACK_URL"}, nil))
	_, err = svc.GetCommandEnv(app)
	require.ErrorIs(t, err, ErrInvalidSettingValue)

	require.NoError(t, database.UpsertSetting(&app, KeyCommandEnvVars, []string{"PATH"}, nil))
	require.NoError(t, database.UpsertSetting(&app, KeyCommandEnvReadOnly, []string{"DATABASE_URL"}, nil))
	_, err = svc.GetCommandEnv(app)
	require.ErrorIs(t, err, ErrInvalidSettingValue)
}

func TestCommandEnvPlan(t *testing.T) {
	readOnly := map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}
	unlimited := CommandEnv{ReadOnly: readOnly}
	limited := CommandEnv{Vars: []string{"PATH", "DATABASE_URL"}, ReadOnly: readOnly}
	for _, c := range []struct {
		env        CommandEnv
		names      []string
		all, write bool
		want       cmdenv.Plan
	}{
		{env: CommandEnv{}, want: cmdenv.Plan{All: true}},
		{env: unlimited, want: cmdenv.Plan{All: true, ReadOnly: readOnly}},
		{env: unlimited, write: true, want: cmdenv.Plan{All: true}},
		{env: unlimited, names: []string{"STRIPE_KEY"}, want: cmdenv.Plan{All: true, ReadOnly: readOnly}},
		{env: limited, want: cmdenv.Plan{Vars: []string{"DATABASE_URL", "PATH"}, ReadOnly: readOnly}},
		{
			env: limited, names: []string{"STRIPE_KEY"}, write: true,
			want: cmdenv.Plan{Vars: []string{"DATABASE_URL", "PATH", "STRIPE_KEY"}},
		},
		{env: limited, all: true, want: cmdenv.Plan{All: true, ReadOnly: readOnly}},
	} {
		plan, err := c.env.Plan(cmdenv.Request{Env: c.names, AllEnvVars: c.all, Write: c.write})
		require.NoError(t, err)
		require.Equalf(t, c.want, plan, "%+v", c)
	}
	// A name the command asks for is checked whether or not the app limits its env vars.
	for _, env := range []CommandEnv{limited, unlimited} {
		_, err := env.Plan(cmdenv.Request{Env: []string{"RACK_URL"}})
		require.ErrorContains(t, err, "no command can have RACK_URL")
	}
}
