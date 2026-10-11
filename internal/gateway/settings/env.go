package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
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

	typed, err := parseTyped(defaultValue, raw)
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

func parseTyped(defaultValue interface{}, raw string) (interface{}, error) {
	switch defaultValue.(type) {
	case bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("must be true or false, got %q", raw)
		}
		return b, nil
	case int:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("must be a whole number, got %q", raw)
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
		return raw, nil
	}
}

// parseEnvList reads a JSON array of strings, or a comma-separated list.
func parseEnvList(raw string) ([]string, error) {
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
// the setting must exist and the value must fit its type. The gateway refuses to start otherwise, instead of
// quietly using the default.
func ValidateEnvironment(environ []string) error {
	var problems []string
	for _, entry := range environ {
		name, raw, _ := strings.Cut(entry, "=")
		isApp, key, ok := settingEnvKey(name)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := parseEnvSetting(isApp, key, strings.TrimSpace(raw)); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("invalid setting environment variables:\n  %s", strings.Join(problems, "\n  "))
}

// settingEnvKey maps a setting environment variable name to its setting key.
func settingEnvKey(name string) (isApp bool, key string, ok bool) {
	if rest, found := strings.CutPrefix(name, globalSettingEnvPrefix); found {
		return false, strings.ToLower(rest), rest != ""
	}
	if rest, found := strings.CutPrefix(name, appSettingEnvPrefix); found {
		if i := strings.LastIndex(rest, appSettingEnvInfix); i > 0 {
			key = rest[i+len(appSettingEnvInfix):]
			return true, strings.ToLower(key), key != ""
		}
	}
	return false, "", false
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
