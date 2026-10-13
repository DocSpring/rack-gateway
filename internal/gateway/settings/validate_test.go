package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAppSettingValue(t *testing.T) {
	valid := map[string]interface{}{
		KeyApprovedDeployCommands:        []interface{}{"bin/pre_release_checks migrate"},
		KeyAuditVisibleEnvVars:           []interface{}{"PORT"},
		KeyProtectedEnvVars:              []string{"DATABASE_URL"},
		AppSettingSecretEnvVars.String(): []interface{}{},
		KeyServiceImagePatterns: map[string]interface{}{
			"*": `docker\.io/docspringcom/app:{{GIT_COMMIT}}-amd64`,
		},
		"some_other_key": map[string]interface{}{"anything": 1},
	}
	for key, value := range valid {
		require.NoErrorf(t, ValidateAppSettingValue(key, value), "key %s", key)
	}

	invalid := map[string]interface{}{
		// The shape the CLI E2E seed used to store: it read back as an empty list.
		KeyApprovedDeployCommands:        map[string]interface{}{"commands": []interface{}{"echo rake db:migrate"}},
		KeyAuditVisibleEnvVars:           map[string]interface{}{"PORT": true},
		KeyProtectedEnvVars:              "DATABASE_URL",
		AppSettingSecretEnvVars.String(): []interface{}{"A", 1},
		KeyServiceImagePatterns:          []interface{}{"docker.io/app:{{GIT_COMMIT}}"},
	}
	for key, value := range invalid {
		require.ErrorIsf(t, ValidateAppSettingValue(key, value), ErrInvalidSettingValue, "key %s", key)
	}

	require.ErrorIs(t, ValidateAppSettingValue(KeyServiceImagePatterns, map[string]interface{}{"web": "app:("}),
		ErrInvalidSettingValue)
	require.ErrorIs(t, ValidateAppSettingValue(KeyServiceImagePatterns, map[string]interface{}{"web": 1}),
		ErrInvalidSettingValue)
}

// The env var lists must hold env var names: a name with quotes or other characters in it matches no key, so the
// list would quietly protect, mask or show nothing.
func TestValidateAppSettingValueRefusesInvalidEnvVarNames(t *testing.T) {
	for _, key := range []string{KeyProtectedEnvVars, AppSettingSecretEnvVars.String(), KeyAuditVisibleEnvVars} {
		require.NoErrorf(t, ValidateAppSettingValue(key, []interface{}{"DATABASE_URL", " port ", "_X1"}), "key %s", key)
		for _, names := range [][]string{
			{`"DATABASE_URL`, `SECRET_KEY_BASE"`}, // a quoted comma-separated value split on its comma
			{"DATABASE-URL"},
			{"1PASSWORD"},
			{"A B"},
			{""},
		} {
			err := ValidateAppSettingValue(key, names)
			require.ErrorIsf(t, err, ErrInvalidSettingValue, "key %s, names %q", key, names)
		}
	}
	// Approved deploy commands are commands, not names.
	require.NoError(t, ValidateAppSettingValue(KeyApprovedDeployCommands, []string{`bin/rails "db:migrate"`}))
}
