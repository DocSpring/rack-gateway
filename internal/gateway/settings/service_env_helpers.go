package settings

import (
	"fmt"
	"strings"
)

// GetProtectedEnvVars returns protected environment variables for an app.
func (s *Service) GetProtectedEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeyProtectedEnvVars)
}

// GetSecretEnvVars returns secret environment variables for an app.
func (s *Service) GetSecretEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeySecretEnvVars)
}

// GetAuditVisibleEnvVars returns the env vars whose values an app's audit log entries may show.
func (s *Service) GetAuditVisibleEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeyAuditVisibleEnvVars)
}

// getEnvVarList reads an env var name list. A stored value that isn't one is an error rather than an empty list:
// callers then treat every key as secret or protected (fail closed), where an empty list would make none of them.
func (s *Service) getEnvVarList(appName, key string) ([]string, error) {
	names, err := s.getListSetting(appName, key, envVarNameList)
	if err != nil {
		return nil, err
	}
	return normalizeEnvVars(names), nil
}

// getListSetting reads an app's list setting with parse, which refuses a value that isn't the expected list.
func (s *Service) getListSetting(
	appName, key string,
	parse func(key string, value interface{}) ([]string, error),
) ([]string, error) {
	setting, err := s.GetAppSetting(appName, key, []string{})
	if err != nil {
		return nil, err
	}
	list, err := parse(key, setting.Value)
	if err != nil {
		return nil, fmt.Errorf("app %s: %w", appName, err)
	}
	return list, nil
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

// GetApprovedDeployCommands returns approved commands for an app. A stored value that isn't a list of strings is
// an error, which allows no commands.
func (s *Service) GetApprovedDeployCommands(appName string) ([]string, error) {
	return s.getListSetting(appName, KeyApprovedDeployCommands, stringList)
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
