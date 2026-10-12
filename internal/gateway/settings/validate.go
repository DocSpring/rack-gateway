package settings

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// ErrInvalidSettingValue means a setting value has the wrong shape for its key.
var ErrInvalidSettingValue = errors.New("invalid setting value")

// stringListKeys are app settings stored as a JSON array of strings. Reading one back fails on any other shape
// (the gateway then treats every env var as secret and protected, and allows no deploy commands), so a value of
// another shape is refused before it is saved.
var stringListKeys = map[string]bool{
	KeyApprovedDeployCommands: true,
	KeyAuditVisibleEnvVars:    true,
	KeyProtectedEnvVars:       true,
	KeySecretEnvVars:          true,
}

// envVarNameKeys are the string lists that name env vars. A name that isn't a valid env var name (one with quotes
// in it, say) matches no key, so the list would quietly protect, mask or show nothing.
var envVarNameKeys = map[string]bool{
	KeyAuditVisibleEnvVars: true,
	KeyProtectedEnvVars:    true,
	KeySecretEnvVars:       true,
}

// ValidateAppSettingValue checks the shape of an app setting value before it is saved.
func ValidateAppSettingValue(key string, value interface{}) error {
	switch {
	case envVarNameKeys[key]:
		_, err := envVarNameList(key, value)
		return err
	case stringListKeys[key]:
		_, err := stringList(key, value)
		return err
	case key == KeyServiceImagePatterns:
		return validateImagePatterns(value)
	default:
		return nil
	}
}

// stringList returns value as a list of strings, or ErrInvalidSettingValue when it has any other shape.
func stringList(key string, value interface{}) ([]string, error) {
	switch v := value.(type) {
	case []string:
		return v, nil
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be a list of strings", ErrInvalidSettingValue, key)
			}
			items = append(items, s)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%w: %s must be a JSON array of strings", ErrInvalidSettingValue, key)
	}
}

// envVarNameList returns value as a list of env var names, or ErrInvalidSettingValue when it isn't a list of
// strings or a name (ignoring surrounding spaces) isn't a valid env var name.
func envVarNameList(key string, value interface{}) ([]string, error) {
	names, err := stringList(key, value)
	if err != nil {
		return nil, err
	}
	var invalid []string
	for _, name := range names {
		if !envutil.ValidName(strings.TrimSpace(name)) {
			invalid = append(invalid, strconv.Quote(name))
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf(
			"%w: %s must list env var names (letters, digits and underscores, not starting with a digit), got %s",
			ErrInvalidSettingValue, key, strings.Join(invalid, ", "),
		)
	}
	return names, nil
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
