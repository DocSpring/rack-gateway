package envutil

import (
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/debugredact"
)

// secretNameParts make an env var name secret wherever they appear in it.
var secretNameParts = []string{"SECRET", "TOKEN", "PASSWORD", "PASSWD", "PASSPHRASE", "CREDENTIAL"}

// secretNameSuffixes make an env var name secret at its end. A plain PASS anywhere would also match names such as
// BYPASS_CACHE or COMPASS_URL, so only a _PASS suffix (SMTP_PASS, DB_PASS) counts.
var secretNameSuffixes = []string{"_KEY", "_KEY_ID", "_PASS"}

// IsSecretKey returns true if key looks secret-like (by pattern) or is in the explicit list. A secret's value is
// masked for callers without convox:secret:read, and changing it needs convox:secret:set.
func IsSecretKey(key string, explicit []string) bool {
	upper := strings.ToUpper(key)
	for _, part := range secretNameParts {
		if strings.Contains(upper, part) {
			return true
		}
	}
	for _, suffix := range secretNameSuffixes {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	for _, k := range explicit {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return true
		}
	}
	return false
}

// HasCredentials reports whether an env var's value carries a credential the name doesn't reveal: a URL with
// credentials in it (scheme://user:password@host, such as a DATABASE_URL), a URL with a secret query parameter
// (?api_key=..., ?token=...; see debugredact.HasSensitiveQuery), or a keyword connection string with a password
// (host=db password=...).
func HasCredentials(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(strings.ToLower(trimmed), "password=") {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	if parsed.User != nil && parsed.User.String() != "" {
		return true
	}
	return parsed.RawQuery != "" && debugredact.HasSensitiveQuery(parsed.RawQuery)
}

// MaskedForReaders reports whether an env var's value is masked for callers without convox:secret:read: a secret
// (IsSecretKey), or a value with credentials in it (HasCredentials). Only a secret needs convox:secret:set to be
// changed, so env:set holders can still change a connection URL they can't read.
func MaskedForReaders(key, value string, explicit []string) bool {
	return IsSecretKey(key, explicit) || HasCredentials(value)
}
