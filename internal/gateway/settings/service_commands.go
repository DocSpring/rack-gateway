package settings

import "fmt"

// GetRequireCommandJustification returns whether every command run in the app (exec, run) needs a justification.
// A stored value that isn't true or false is an error, which requires one.
func (s *Service) GetRequireCommandJustification(appName string) (bool, error) {
	setting, err := s.GetAppSetting(appName, KeyRequireCommandJustification, false)
	if err != nil {
		return true, err
	}
	required, err := boolValue(KeyRequireCommandJustification, setting.Value)
	if err != nil {
		return true, fmt.Errorf("app %s: %w", appName, err)
	}
	return required, nil
}
