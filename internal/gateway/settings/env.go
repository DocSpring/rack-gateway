package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ErrSetByEnvironment means a setting is defined by an environment variable (the deployment config), so it can't
// be changed through the gateway.
var ErrSetByEnvironment = errors.New("setting is set by an environment variable")

// Environment variable names: RGW_SETTING_<KEY> for global settings, RGW_APP_<APP>_SETTING_<KEY> for app settings.
const (
	globalSettingEnvPrefix = "RGW_SETTING_"
	appSettingEnvPrefix    = "RGW_APP_"
	appSettingEnvInfix     = "_SETTING_"
)

// envSetting returns the setting defined by its environment variable, or ok=false when the variable isn't set. An
// environment variable overrides the database, so the deployment config can pin a setting.
func envSetting(appName *string, key string) (setting *Setting, ok bool, err error) {
	envVar := getEnvVarName(appName, key)
	raw := strings.TrimSpace(os.Getenv(envVar))
	if raw == "" {
		return nil, false, nil
	}
	value, err := parseEnvSetting(appName != nil, key, raw)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", envVar, err)
	}
	return &Setting{Value: value, Source: SourceEnv, EnvVar: envVar}, true, nil
}

// checkEnvDefined decides whether value can be stored for a setting. A setting its environment variable defines
// can't be changed (ErrSetByEnvironment); storing the value it already has is skipped (store=false), so forms
// that submit every field still work.
func checkEnvDefined(appName *string, key string, value interface{}) (store bool, err error) {
	setting, defined, err := envSetting(appName, key)
	if !defined {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	normalized, err := toJSONShape(value)
	if err == nil && reflect.DeepEqual(normalized, setting.Value) {
		return false, nil
	}
	return false, fmt.Errorf("%w: %s is set by %s; change it in the deployment config",
		ErrSetByEnvironment, key, setting.EnvVar)
}

// CheckEnvironment refuses a change to a setting its environment variable defines (ErrSetByEnvironment). appName
// is nil for a global setting. Submitting the value the setting already has is allowed.
func CheckEnvironment(appName *string, key string, value interface{}) error {
	_, err := checkEnvDefined(appName, key, value)
	return err
}

// parseEnvSetting parses an environment variable value strictly by the setting's type, and returns it in the
// shape a JSON-decoded database value has (float64, []interface{}, map[string]interface{}), so callers can't tell
// the two apart.
func parseEnvSetting(isApp bool, key, raw string) (interface{}, error) {
	defaults := DefaultGlobalSettings
	if isApp {
		defaults = DefaultAppSettings
	}
	defaultValue, known := defaults[key]
	if !known {
		return nil, fmt.Errorf("unknown setting %q", key)
	}

	typed, err := parseTyped(key, defaultValue, raw)
	if err != nil {
		return nil, err
	}
	if isApp {
		if err := ValidateAppSettingValue(key, typed); err != nil {
			return nil, err
		}
	}
	return toJSONShape(typed)
}

// allowedSettingValues lists the only values some string settings can take.
var allowedSettingValues = map[string][]string{
	KeyVerifyGitCommitMode: {VerifyGitCommitModeBranch, VerifyGitCommitModeLatest},
}

func parseTyped(key string, defaultValue interface{}, raw string) (interface{}, error) {
	switch defaultValue.(type) {
	case bool:
		return parseEnvBool(raw)
	case int:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("must be a whole number of at least 1, got %q", raw)
		}
		return n, nil
	case []string:
		return parseEnvList(raw)
	case map[string]string:
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, fmt.Errorf("must be a JSON object of strings: %w", err)
		}
		return m, nil
	default: // strings, including optional ones with a nil default
		return parseEnvString(key, raw)
	}
}

func parseEnvBool(raw string) (bool, error) {
	switch strings.ToLower(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("must be true or false, got %q", raw)
	}
}

// parseEnvString reads a string value: plain, or JSON-encoded (as Terraform's jsonencode writes every value). Any
// other quoting would become part of the value, so it's refused, and so is null (unset the variable instead).
func parseEnvString(key, raw string) (string, error) {
	if strings.HasPrefix(raw, `"`) {
		if err := json.Unmarshal([]byte(raw), &raw); err != nil {
			return "", fmt.Errorf("must be a plain value or a JSON string, got %s", raw)
		}
	} else if strings.HasPrefix(raw, "'") || raw == "null" {
		return "", fmt.Errorf("must be a plain value or a JSON string, got %s", raw)
	}
	allowed, restricted := allowedSettingValues[key]
	if restricted && !slices.Contains(allowed, raw) {
		return "", fmt.Errorf("must be one of %s, got %q", strings.Join(allowed, ", "), raw)
	}
	return raw, nil
}

// parseEnvList reads a JSON array of strings, or a comma-separated list. A quoted value is refused: a JSON string
// ("A,B", as jsonencode writes a string) or a single-quoted one would split into names with quotes in them, which
// match nothing. So is null (unset the variable instead).
func parseEnvList(raw string) ([]string, error) {
	if strings.HasPrefix(raw, `"`) || strings.HasPrefix(raw, "'") || raw == "null" {
		return nil, fmt.Errorf("must be a JSON array of strings or a comma-separated list, got %s", raw)
	}
	if strings.HasPrefix(raw, "[") {
		var list []string
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, fmt.Errorf("must be a JSON array of strings or a comma-separated list: %w", err)
		}
		return list, nil
	}
	list := []string{}
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list, nil
}

func toJSONShape(value interface{}) (interface{}, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var shaped interface{}
	if err := json.Unmarshal(data, &shaped); err != nil {
		return nil, err
	}
	return shaped, nil
}

// ValidateEnvironment checks every setting environment variable (RGW_SETTING_*, RGW_APP_*_SETTING_*) in environ:
// the name must be one the gateway reads, the setting must exist and the value must fit its type. The gateway
// refuses to start otherwise, instead of quietly using another value.
func ValidateEnvironment(environ []string) error {
	var problems []string
	for _, entry := range environ {
		name, raw, _ := strings.Cut(entry, "=")
		if !looksLikeSettingEnv(name) || strings.TrimSpace(raw) == "" {
			continue
		}
		if err := validateSettingEnv(name, strings.TrimSpace(raw)); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("invalid setting environment variables:\n  %s", strings.Join(problems, "\n  "))
}

// looksLikeSettingEnv reports a name meant as a setting variable, in any case.
func looksLikeSettingEnv(name string) bool {
	upper := strings.ToUpper(name)
	return strings.HasPrefix(upper, globalSettingEnvPrefix) ||
		(strings.HasPrefix(upper, appSettingEnvPrefix) && strings.Contains(upper, appSettingEnvInfix))
}

func validateSettingEnv(name, raw string) error {
	isApp, key, err := settingEnvKey(name)
	if err != nil {
		return err
	}
	_, err = parseEnvSetting(isApp, key, raw)
	return err
}

// envAppNamePattern is an app name as setting variables spell it: upper case, with dashes written as underscores.
var envAppNamePattern = regexp.MustCompile(`^[A-Z0-9_]+$`)

// settingEnvKey maps a setting environment variable name to its setting key. The gateway only reads names in the
// form getEnvVarName builds, so any other spelling is an error rather than a variable that's silently ignored.
func settingEnvKey(name string) (isApp bool, key string, err error) {
	if name != strings.ToUpper(name) {
		return false, "", errors.New("setting variable names are upper case")
	}
	if rest, found := strings.CutPrefix(name, globalSettingEnvPrefix); found {
		return false, strings.ToLower(rest), nil
	}
	rest := strings.TrimPrefix(name, appSettingEnvPrefix)
	i := strings.LastIndex(rest, appSettingEnvInfix)
	if i <= 0 {
		return false, "", errors.New("missing the app name (RGW_APP_<APP>_SETTING_<KEY>)")
	}
	if !envAppNamePattern.MatchString(rest[:i]) {
		return false, "", errors.New("the app name must be letters, digits and underscores (dashes become underscores)")
	}
	return true, strings.ToLower(rest[i+len(appSettingEnvInfix):]), nil
}

// ShadowedSettings lists the stored settings an environment variable now overrides. They have no effect, so they
// can be deleted.
func (s *Service) ShadowedSettings() ([]string, error) {
	keys, err := s.db.ListSettingKeys()
	if err != nil {
		return nil, err
	}
	var shadowed []string
	for _, k := range keys {
		envVar := getEnvVarName(k.AppName, k.Key)
		if strings.TrimSpace(os.Getenv(envVar)) == "" {
			continue
		}
		scope := "global"
		if k.AppName != nil {
			scope = "app " + *k.AppName
		}
		shadowed = append(shadowed, fmt.Sprintf("%s (%s), overridden by %s", k.Key, scope, envVar))
	}
	return shadowed, nil
}
