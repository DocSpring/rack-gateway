package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAppSettingValue(t *testing.T) {
	valid := map[string]interface{}{
		KeyApprovedDeployCommands: []interface{}{"bin/pre_release_checks migrate"},
		KeyProtectedEnvVars:       []string{"DATABASE_URL"},
		KeySecretEnvVars:          []interface{}{},
		KeyServiceImagePatterns:   map[string]interface{}{"*": `docker\.io/docspringcom/app:{{GIT_COMMIT}}-amd64`},
		"some_other_key":          map[string]interface{}{"anything": 1},
	}
	for key, value := range valid {
		require.NoErrorf(t, ValidateAppSettingValue(key, value), "key %s", key)
	}

	invalid := map[string]interface{}{
		// The shape the CLI E2E seed used to store: it read back as an empty list.
		KeyApprovedDeployCommands: map[string]interface{}{"commands": []interface{}{"echo rake db:migrate"}},
		KeyProtectedEnvVars:       "DATABASE_URL",
		KeySecretEnvVars:          []interface{}{"A", 1},
		KeyServiceImagePatterns:   []interface{}{"docker.io/app:{{GIT_COMMIT}}"},
	}
	for key, value := range invalid {
		require.ErrorIsf(t, ValidateAppSettingValue(key, value), ErrInvalidSettingValue, "key %s", key)
	}

	require.ErrorIs(t, ValidateAppSettingValue(KeyServiceImagePatterns, map[string]interface{}{"web": "app:("}),
		ErrInvalidSettingValue)
	require.ErrorIs(t, ValidateAppSettingValue(KeyServiceImagePatterns, map[string]interface{}{"web": 1}),
		ErrInvalidSettingValue)
}
