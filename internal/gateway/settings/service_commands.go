package settings

import "fmt"

// GetRequireCommandJustification returns whether every command run in the app (exec, run) needs a justification:
// when the gateway-wide require_command_justification is on, or the app's own is. An app can't opt out of the
// gateway-wide setting. A stored value that isn't true or false is an error, which requires one.
func (s *Service) GetRequireCommandJustification(appName string) (bool, error) {
	required, err := readGlobal(s, KeyRequireCommandJustification, nil, boolValue)
	if err != nil || required {
		return true, err
	}
	setting, err := s.GetAppSetting(appName, KeyRequireCommandJustification, false)
	if err != nil {
		return true, err
	}
	required, err = boolValue(KeyRequireCommandJustification, setting.Value)
	if err != nil {
		return true, fmt.Errorf("app %s: %w", appName, err)
	}
	return required, nil
}

// GetApprovedDeployCommands returns the commands a deploy approval may run in the app (approved_deploy_commands).
// A stored value that isn't a list of approved commands is an error, which allows none.
func (s *Service) GetApprovedDeployCommands(appName string) ([]ApprovedCommand, error) {
	setting, err := s.GetAppSetting(appName, KeyApprovedDeployCommands, []ApprovedCommand{})
	if err != nil {
		return nil, err
	}
	commands, err := approvedCommandList(setting.Value)
	if err != nil {
		return nil, fmt.Errorf("app %s: %w", appName, err)
	}
	return commands, nil
}

// GetCommandEnv returns what the app's settings say about the env vars of its commands (command_env_vars and
// command_env_read_only). A stored value of the wrong shape is an error, which refuses the app's commands: running
// one with every env var would give it what the settings keep from it.
func (s *Service) GetCommandEnv(appName string) (CommandEnv, error) {
	vars, err := s.GetAppSetting(appName, KeyCommandEnvVars, []string{})
	if err != nil {
		return CommandEnv{}, err
	}
	readOnly, err := s.GetAppSetting(appName, KeyCommandEnvReadOnly, map[string]string{})
	if err != nil {
		return CommandEnv{}, err
	}
	env, err := CommandEnvFromValues(vars.Value, readOnly.Value)
	if err != nil {
		return CommandEnv{}, fmt.Errorf("app %s: %w", appName, err)
	}
	return env, nil
}
