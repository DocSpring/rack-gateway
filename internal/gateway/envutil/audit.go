package envutil

import (
	"encoding/json"
	"strings"
)

// RedactedValue stands in for an env var value in audit log entries, and so in Slack notifications.
const RedactedValue = "[REDACTED]"

// AuditVisibleKeys are the env vars, upper-cased, whose values an app's audit log entries may show: the app's
// audit_visible_env_vars, less any secret or protected key. Every other value is redacted. The zero value shows
// none.
type AuditVisibleKeys map[string]struct{}

// AuditSettings reads the app settings that decide which env var values audit log entries may show.
type AuditSettings interface {
	GetAuditVisibleEnvVars(appName string) ([]string, error)
	GetSecretEnvVars(appName string) ([]string, error)
	GetProtectedEnvVars(appName string) ([]string, error)
}

// LoadAuditVisibleKeys reads the app's audit_visible_env_vars, less its secret_env_vars, its protected_env_vars,
// secretNames (CONVOX_SECRET_ENV_VARS) and any key that looks secret. When a setting can't be read, it returns
// the error and keys that show nothing.
func LoadAuditVisibleKeys(s AuditSettings, app string, secretNames []string) (AuditVisibleKeys, error) {
	visible, err := s.GetAuditVisibleEnvVars(app)
	if err != nil || len(visible) == 0 {
		return AuditVisibleKeys{}, err
	}
	secret, err := s.GetSecretEnvVars(app)
	if err != nil {
		return AuditVisibleKeys{}, err
	}
	protected, err := s.GetProtectedEnvVars(app)
	if err != nil {
		return AuditVisibleKeys{}, err
	}
	hidden := make([]string, 0, len(secret)+len(protected)+len(secretNames))
	hidden = append(hidden, secret...)
	hidden = append(hidden, protected...)
	hidden = append(hidden, secretNames...)
	return newAuditVisibleKeys(visible, hidden), nil
}

// newAuditVisibleKeys returns the visible keys, without the hidden ones and any key that looks secret.
func newAuditVisibleKeys(visible, hidden []string) AuditVisibleKeys {
	keys := AuditVisibleKeys{}
	for _, name := range visible {
		key := strings.ToUpper(strings.TrimSpace(name))
		if key == "" || IsSecretKey(key, hidden) {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys
}

// envChangeDetails is the details of an env change's audit log entry. A struct keeps "old" before "new".
type envChangeDetails struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// AuditDetails returns the details JSON of the audit log entry for d. Its values are shown only when its key is
// visible, the change is neither secret nor protected, and neither value has credentials in it (HasCredentials);
// otherwise both are RedactedValue.
func (v AuditVisibleKeys) AuditDetails(d EnvDiff) string {
	details := envChangeDetails{Old: RedactedValue, New: RedactedValue}
	if v.shows(d) {
		details = envChangeDetails{Old: d.OldVal, New: d.NewVal}
	}
	data, err := json.Marshal(details)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (v AuditVisibleKeys) shows(d EnvDiff) bool {
	if d.Secret || d.Protected || HasCredentials(d.OldVal) || HasCredentials(d.NewVal) {
		return false
	}
	_, ok := v[strings.ToUpper(strings.TrimSpace(d.Key))]
	return ok
}
