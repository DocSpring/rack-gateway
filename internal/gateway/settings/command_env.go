package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
)

// ApprovedCommand is a command a deploy approval may run (an entry of approved_deploy_commands), with the env vars
// it runs with. The command's own flags can't change them: under a deploy approval, its entry decides.
type ApprovedCommand struct {
	// Command is the command, as it's run.
	Command string `json:"command"`
	// Env names the env vars it gets besides the app's command_env_vars.
	Env []string `json:"env,omitempty"`
	// AllEnvVars gives it every env var (except RACK_URL), as --all-env-vars does.
	AllEnvVars bool `json:"all_env_vars,omitempty"`
	// Write gives it the real values of the app's read-only env vars (command_env_read_only), as --write does.
	Write bool `json:"write,omitempty"`
}

// CommandEnv is what an app's settings say about the env vars of the commands run in it.
type CommandEnv struct {
	// Vars are the env vars every command gets (command_env_vars). When there are none, every command gets every
	// env var (except RACK_URL).
	Vars []string
	// ReadOnly maps an env var to the one a command gets its value from unless it's run with --write
	// (command_env_read_only).
	ReadOnly map[string]string
}

// Request is what the entry gives its command, as the flags of a command run by hand would ask for it.
func (c ApprovedCommand) Request() cmdenv.Request {
	return cmdenv.Request{Env: c.Env, AllEnvVars: c.AllEnvVars, Write: c.Write}
}

// Plan returns the env vars a command that asks for req gets: the app's and the ones it names, or every env var
// when it asks for them all or the app doesn't limit them; the substitutes of the read-only vars unless it asks to
// write.
func (e CommandEnv) Plan(req cmdenv.Request) (cmdenv.Plan, error) {
	if err := req.Check(); err != nil {
		return cmdenv.Plan{}, err
	}
	readOnly := e.ReadOnly
	if req.Write {
		readOnly = nil
	}
	vars := append(append([]string(nil), e.Vars...), req.Env...)
	plan, err := cmdenv.New(req.AllEnvVars || len(e.Vars) == 0, vars, readOnly)
	if err != nil || plan.All {
		return plan, err
	}
	for _, name := range req.Env {
		if !slices.Contains(e.Vars, name) && !slices.Contains(plan.Added, name) {
			plan.Added = append(plan.Added, name)
		}
	}
	slices.Sort(plan.Added)
	return plan, nil
}

// CommandEnvFromValues reads command_env_vars and command_env_read_only from their values, as the settings API
// returns them (JSON-decoded). A value of the wrong shape is an error (ErrInvalidSettingValue).
func CommandEnvFromValues(vars, readOnly interface{}) (CommandEnv, error) {
	var env CommandEnv
	var err error
	if env.Vars, err = commandEnvVarNames(vars); err != nil {
		return CommandEnv{}, err
	}
	if env.ReadOnly, err = readOnlyEnvMap(readOnly); err != nil {
		return CommandEnv{}, err
	}
	return env, nil
}

// Limits reports whether the app keeps anything from its commands: env vars they don't get, or the real values of
// read-only ones.
func (e CommandEnv) Limits() bool {
	return len(e.Vars) > 0 || len(e.ReadOnly) > 0
}

// commandEnvVarNames returns command_env_vars's value as env var names, or ErrInvalidSettingValue when it isn't a
// list of env var names (surrounding spaces aside), or it names RACK_URL. Names keep their case: a process's env
// var names are case-sensitive.
func commandEnvVarNames(value interface{}) ([]string, error) {
	names, err := stringList(KeyCommandEnvVars, value)
	if err != nil {
		return nil, err
	}
	trimmed := make([]string, 0, len(names))
	for _, name := range names {
		trimmed = append(trimmed, strings.TrimSpace(name))
	}
	if err := cmdenv.CheckNames(trimmed); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidSettingValue, KeyCommandEnvVars, err)
	}
	return trimmed, nil
}

// readOnlyEnvMap returns command_env_read_only's value, or ErrInvalidSettingValue when it isn't an object of env
// var name to env var name that cmdenv.CheckReadOnly accepts.
func readOnlyEnvMap(value interface{}) (map[string]string, error) {
	readOnly, err := stringMap(KeyCommandEnvReadOnly, value, "env var name to the env var it reads instead")
	if err != nil {
		return nil, err
	}
	if err := cmdenv.CheckReadOnly(readOnly); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidSettingValue, KeyCommandEnvReadOnly, err)
	}
	return readOnly, nil
}

// stringMap returns value as an object of strings, or ErrInvalidSettingValue (saying the object holds what) when
// it has any other shape.
func stringMap(key string, value interface{}, what string) (map[string]string, error) {
	switch v := value.(type) {
	case map[string]string:
		return v, nil
	case map[string]interface{}:
		m := make(map[string]string, len(v))
		for name, raw := range v {
			s, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %s: the value for %s must be a string", ErrInvalidSettingValue, key, name)
			}
			m[name] = s
		}
		return m, nil
	default:
		return nil, fmt.Errorf("%w: %s must be an object of %s", ErrInvalidSettingValue, key, what)
	}
}

// approvedCommandList returns approved_deploy_commands's value, or ErrInvalidSettingValue when it isn't a JSON
// array of approved commands: objects with a command and only the fields ApprovedCommand has, each command one the
// gateway can run (see cmdenv.CheckCommand) and listed once, env naming env vars, and not both env and
// all_env_vars.
func approvedCommandList(value interface{}) ([]ApprovedCommand, error) {
	commands, err := decodeApprovedCommands(value)
	if err == nil {
		err = checkApprovedCommands(commands)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidSettingValue, KeyApprovedDeployCommands, err)
	}
	return commands, nil
}

// decodeApprovedCommands reads value (as JSON decodes it, or already typed) as approved commands, refusing a field
// ApprovedCommand doesn't have.
func decodeApprovedCommands(value interface{}) ([]ApprovedCommand, error) {
	if commands, ok := value.([]ApprovedCommand); ok {
		return commands, nil
	}
	data, err := json.Marshal(value)
	if err != nil || value == nil || !bytes.HasPrefix(data, []byte("[")) {
		return nil, errors.New(`must be a JSON array of objects like {"command": "bin/migrate"}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var commands []ApprovedCommand
	if err := decoder.Decode(&commands); err != nil {
		return nil, fmt.Errorf("each entry must be an object with a command, and optionally env (a list of env var "+
			"names), all_env_vars and write (true or false): %w", err)
	}
	return commands, nil
}

// checkApprovedCommands refuses a command the gateway can't run or that's listed twice, an env var name that isn't
// one, and env with all_env_vars.
func checkApprovedCommands(commands []ApprovedCommand) error {
	seen := map[string]bool{}
	for i, command := range commands {
		text := strings.TrimSpace(command.Command)
		if err := cmdenv.CheckCommand(text); err != nil {
			return fmt.Errorf("entry %d: %w", i+1, err)
		}
		if seen[text] {
			return fmt.Errorf("%q is listed more than once", text)
		}
		seen[text] = true
		if err := cmdenv.CheckNames(command.Env); err != nil {
			return fmt.Errorf("%q: %w", text, err)
		}
		if command.AllEnvVars && len(command.Env) > 0 {
			return fmt.Errorf("%q: all_env_vars gives it every env var, so it can't have env as well", text)
		}
	}
	return nil
}

// parseEnvApprovedCommands reads approved_deploy_commands from its environment variable: a JSON array of objects.
func parseEnvApprovedCommands(raw string) ([]ApprovedCommand, error) {
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf(`must be a JSON array of objects like {"command": "bin/migrate"}: %w`, err)
	}
	return approvedCommandList(value)
}
