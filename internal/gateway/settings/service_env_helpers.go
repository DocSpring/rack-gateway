package settings

import "strings"

// GetProtectedEnvVars returns protected environment variables for an app.
func (s *Service) GetProtectedEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeyProtectedEnvVars)
}

// GetSecretEnvVars returns secret environment variables for an app.
func (s *Service) GetSecretEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeySecretEnvVars)
}

func (s *Service) getEnvVarList(appName, key string) ([]string, error) {
	setting, err := s.GetAppSetting(appName, key, []string{})
	if err != nil {
		return []string{}, err
	}
	return normalizeEnvVars(extractStringSlice(setting.Value)), nil
}

func extractStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// normalizeEnvVars normalizes env var names (uppercase, unique).
func normalizeEnvVars(vars []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(vars))
	for _, v := range vars {
		normalized := strings.TrimSpace(strings.ToUpper(v))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; !ok {
			seen[normalized] = struct{}{}
			result = append(result, normalized)
		}
	}
	return result
}

// GetApprovedDeployCommands returns approved commands for an app.
func (s *Service) GetApprovedDeployCommands(appName string) ([]string, error) {
	setting, err := s.GetAppSetting(appName, KeyApprovedDeployCommands, []string(nil))
	if err != nil {
		return []string{}, err
	}

	if setting.Value == nil {
		return []string{}, nil
	}

	result := extractStringSlice(setting.Value)
	if result == nil {
		return []string{}, nil
	}
	return result, nil
}

// GetServiceImagePatterns returns service image patterns for an app.
func (s *Service) GetServiceImagePatterns(appName string) (map[string]string, error) {
	setting, err := s.GetAppSetting(appName, KeyServiceImagePatterns, map[string]string(nil))
	if err != nil {
		return map[string]string{}, err
	}

	if setting.Value == nil {
		return map[string]string{}, nil
	}

	// Handle both map[string]interface{} and map[string]string from JSON unmarshaling
	switch val := setting.Value.(type) {
	case map[string]string:
		return val, nil
	case map[string]interface{}:
		result := make(map[string]string)
		for k, v := range val {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
		return result, nil
	default:
		return map[string]string{}, nil
	}
}
