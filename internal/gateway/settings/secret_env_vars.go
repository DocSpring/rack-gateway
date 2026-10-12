package settings

import (
	"fmt"
	"strings"
)

// MaskedNamesEnvVar is the environment variable that lists env var names every app treats as secret, on top of
// each app's secret_env_vars: a JSON array of strings or a comma-separated list, like the list settings.
const MaskedNamesEnvVar = "CONVOX_SECRET_ENV_VARS"

// SecretEnvVarNames returns the names CONVOX_SECRET_ENV_VARS lists, upper-cased. getenv is os.Getenv outside tests.
// A value that isn't a list of env var names is an error, which callers treat as every key being secret: a quoted
// value would otherwise split into names with quotes in them, which match no key. The gateway refuses to start
// with one (ValidateEnvironment).
func SecretEnvVarNames(getenv func(string) string) ([]string, error) {
	names, err := parseSecretEnvVarNames(strings.TrimSpace(getenv(MaskedNamesEnvVar)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", MaskedNamesEnvVar, err)
	}
	return names, nil
}

// parseSecretEnvVarNames parses a CONVOX_SECRET_ENV_VARS value; "" lists no names.
func parseSecretEnvVarNames(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	names, err := parseEnvList(raw)
	if err != nil {
		return nil, err
	}
	if invalid := invalidEnvVarNames(names); invalid != "" {
		return nil, fmt.Errorf("must list %s, got %s", envVarNameRule, invalid)
	}
	return normalizeEnvVars(names), nil
}
