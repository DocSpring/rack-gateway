package settings

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrInvalidSettingValue means a setting value has the wrong shape for its key.
var ErrInvalidSettingValue = errors.New("invalid setting value")

// stringListKeys are app settings stored as a JSON array of strings. A value of any other shape would
// read back as an empty list, and an empty approved_deploy_commands list allows no commands.
var stringListKeys = map[string]bool{
	KeyApprovedDeployCommands: true,
	KeyProtectedEnvVars:       true,
	KeySecretEnvVars:          true,
}

// ValidateAppSettingValue checks the shape of an app setting value before it is saved.
func ValidateAppSettingValue(key string, value interface{}) error {
	switch {
	case stringListKeys[key]:
		return validateStringList(key, value)
	case key == KeyServiceImagePatterns:
		return validateImagePatterns(value)
	default:
		return nil
	}
}

func validateStringList(key string, value interface{}) error {
	switch v := value.(type) {
	case []string:
		return nil
	case []interface{}:
		for _, item := range v {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("%w: %s must be a list of strings", ErrInvalidSettingValue, key)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: %s must be a JSON array of strings", ErrInvalidSettingValue, key)
	}
}

// validateImagePatterns requires an object of service name to pattern, where each pattern compiles once
// {{GIT_COMMIT}} is substituted.
func validateImagePatterns(value interface{}) error {
	patterns := map[string]string{}
	switch v := value.(type) {
	case map[string]string:
		patterns = v
	case map[string]interface{}:
		for service, raw := range v {
			pattern, ok := raw.(string)
			if !ok {
				return fmt.Errorf("%w: image pattern for %s must be a string", ErrInvalidSettingValue, service)
			}
			patterns[service] = pattern
		}
	default:
		return fmt.Errorf("%w: %s must be an object of service name to pattern", ErrInvalidSettingValue,
			KeyServiceImagePatterns)
	}
	services := make([]string, 0, len(patterns))
	for service := range patterns {
		services = append(services, service)
	}
	sort.Strings(services)
	for _, service := range services {
		pattern := strings.ReplaceAll(patterns[service], "{{GIT_COMMIT}}", "0")
		if _, err := regexp.Compile("^(?:" + pattern + ")$"); err != nil {
			return fmt.Errorf("%w: image pattern for %s: %v", ErrInvalidSettingValue, service, err)
		}
	}
	return nil
}
