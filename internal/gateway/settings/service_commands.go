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
