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

// envVarNameRule says what the env var name lists must hold, for error messages.
const envVarNameRule = "env var names (letters, digits and underscores, not starting with a digit)"

// envVarNameList returns value as a list of env var names, or ErrInvalidSettingValue when it isn't a list of
// strings or a name (ignoring surrounding spaces) isn't a valid env var name.
func envVarNameList(key string, value interface{}) ([]string, error) {
	names, err := stringList(key, value)
	if err != nil {
		return nil, err
	}
	if invalid := invalidEnvVarNames(names); invalid != "" {
		return nil, fmt.Errorf("%w: %s must list %s, got %s", ErrInvalidSettingValue, key, envVarNameRule, invalid)
	}
	return names, nil
}

// invalidEnvVarNames returns the names (ignoring surrounding spaces) that aren't valid env var names, quoted and
// comma-separated, or "" when they all are.
func invalidEnvVarNames(names []string) string {
	var invalid []string
	for _, name := range names {
		if !envutil.ValidName(strings.TrimSpace(name)) {
			invalid = append(invalid, strconv.Quote(name))
		}
	}
	return strings.Join(invalid, ", ")
}

// validateImagePatterns requires an object of service name to pattern, where each pattern compiles.
func validateImagePatterns(value interface{}) error {
	_, err := imagePatternMap(value)
	return err
}

// imagePatternMap returns value as a map of service name to image pattern, or ErrInvalidSettingValue when it isn't
// an object of strings or a pattern doesn't compile (with {{GIT_COMMIT}} standing for a commit).
func imagePatternMap(value interface{}) (map[string]string, error) {
	patterns := map[string]string{}
	switch v := value.(type) {
	case map[string]string:
		patterns = v
	case map[string]interface{}:
		for service, raw := range v {
			pattern, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("%w: image pattern for %s must be a string", ErrInvalidSettingValue, service)
			}
			patterns[service] = pattern
		}
	default:
		return nil, fmt.Errorf("%w: %s must be an object of service name to pattern", ErrInvalidSettingValue,
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
			return nil, fmt.Errorf("%w: image pattern for %s: %v", ErrInvalidSettingValue, service, err)
		}
	}
	return patterns, nil
}
