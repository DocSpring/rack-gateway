package settings

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// ErrInvalidSettingValue means a setting value has the wrong shape for its key.
var ErrInvalidSettingValue = errors.New("invalid setting value")

// allowedSettingValues lists the only values some string settings can take.
var allowedSettingValues = map[string][]string{
	KeyVerifyGitCommitMode: {VerifyGitCommitModeBranch, VerifyGitCommitModeLatest},
}

// settingMaximums bounds the numeric settings: each is a whole number from 1 to its maximum, so its duration is
// one that makes sense (and can't overflow).
var settingMaximums = map[string]int{
	KeyDeployApprovalWindowMinutes:            7 * 24 * 60, // a week
	KeySessionTimeoutMinutes:                  24 * 60,     // a day
	GlobalSettingStepUpWindowMinutes.String(): 24 * 60,
	KeyTrustedDeviceTTLDays:                   365,
}

// ValidateSettingValue checks a value against its setting's type before it's saved: true or false, a whole number
// in the setting's range, a string (or null, which reads as the default) that's one of the values the setting
// allows, or a list or object of the right shape. appName is nil for a global setting. An unknown setting is
// refused too.
func ValidateSettingValue(appName *string, key string, value interface{}) error {
	return checkSettingValue(appName != nil, key, value)
}

func checkSettingValue(isApp bool, key string, value interface{}) error {
	defaults := DefaultGlobalSettings
	if isApp {
		defaults = DefaultAppSettings
	}
	defaultValue, known := defaults[key]
	if !known {
		return fmt.Errorf("%w: unknown setting %s", ErrInvalidSettingValue, key)
	}
	switch defaultValue.(type) {
	case bool:
		_, err := boolValue(key, value)
		return err
	case int:
		_, err := wholeNumber(key, value)
		return err
	case []string, map[string]string:
		return validateCollection(key, value)
	case []ApprovedCommand:
		_, err := approvedCommandList(value)
		return err
	default: // strings, including optional ones with a nil default
		return validateString(key, value)
	}
}

// boolValue returns a boolean setting's value, or ErrInvalidSettingValue when it isn't true or false.
func boolValue(key string, value interface{}) (bool, error) {
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s must be true or false", ErrInvalidSettingValue, key)
	}
	return b, nil
}

// wholeNumber returns a numeric setting's value, or ErrInvalidSettingValue when it isn't a whole number from 1 to
// the setting's maximum. JSON numbers decode as float64.
func wholeNumber(key string, value interface{}) (int, error) {
	var n float64 // any other type stays 0, which is refused
	switch v := value.(type) {
	case int:
		n = float64(v)
	case float64:
		n = v
	}
	maximum := settingMaximums[key]
	if n != math.Trunc(n) || n < 1 || n > float64(maximum) {
		return 0, fmt.Errorf("%w: %s must be a whole number from 1 to %d", ErrInvalidSettingValue, key, maximum)
	}
	return int(n), nil
}

// validateString requires a string, or null (which reads as the default), and one of the allowed values for a
// setting that has them.
func validateString(key string, value interface{}) error {
	if value == nil {
		return nil
	}
	s, ok := value.(string)
	if !ok {
		return fmt.Errorf("%w: %s must be a string", ErrInvalidSettingValue, key)
	}
	if allowed, restricted := allowedSettingValues[key]; restricted && !slices.Contains(allowed, s) {
		return fmt.Errorf("%w: %s must be one of %s, got %q", ErrInvalidSettingValue, key,
			strings.Join(allowed, ", "), s)
	}
	return nil
}

// stringListKeys are app settings stored as a JSON array of strings. Reading one back fails on any other shape
// (the gateway then treats every env var as secret and protected), so a value of another shape is refused before
// it is saved.
var stringListKeys = map[string]bool{
	KeyAuditVisibleEnvVars:           true,
	KeyProtectedEnvVars:              true,
	AppSettingSecretEnvVars.String(): true,
}

// envVarNameKeys are the string lists that name env vars. A name that isn't a valid env var name (one with quotes
// in it, say) matches no key, so the list would quietly protect, mask or show nothing.
var envVarNameKeys = map[string]bool{
	KeyAuditVisibleEnvVars:           true,
	KeyProtectedEnvVars:              true,
	AppSettingSecretEnvVars.String(): true,
}

// validateCollection checks the shape of a list or object setting.
func validateCollection(key string, value interface{}) error {
	switch {
	case envVarNameKeys[key]:
		_, err := envVarNameList(key, value)
		return err
	case stringListKeys[key]:
		_, err := stringList(key, value)
		return err
	case key == KeyCommandEnvVars:
		_, err := commandEnvVarNames(value)
		return err
	case key == KeyCommandEnvReadOnly:
		_, err := readOnlyEnvMap(value)
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
	patterns, err := stringMap(KeyServiceImagePatterns, value, "service name to pattern")
	if err != nil {
		return nil, err
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
