package db

import (
	"net/url"
	"os"
	"strings"
)

// sslModeStrength orders libpq's sslmodes from the weakest to the strictest.
var sslModeStrength = map[string]int{
	"disable": 0, "allow": 1, "prefer": 2, "require": 3, "verify-ca": 4, "verify-full": 5,
}

// keepsStricterSSLMode reports whether a DSN's sslmode is stricter than PGSSLMODE's, so PGSSLMODE mustn't replace it:
// it may raise require to verify-full, but never lower verify-full to require.
func keepsStricterSSLMode(current, requested string) bool {
	currentStrength, known := sslModeStrength[strings.ToLower(current)]
	requestedStrength, requestedKnown := sslModeStrength[strings.ToLower(requested)]
	return known && requestedKnown && currentStrength > requestedStrength
}

// ensureSSLMode sets the DSN's sslmode to PGSSLMODE when that's set and isn't weaker than the DSN's own, and
// otherwise leaves the DSN alone.
func ensureSSLMode(dsn string) string {
	s := strings.TrimSpace(dsn)
	if s == "" {
		return dsn
	}
	// Only mutate sslmode when PGSSLMODE is explicitly set
	mode := strings.TrimSpace(os.Getenv("PGSSLMODE"))
	if mode == "" {
		return dsn
	}

	lower := strings.ToLower(s)

	// URL DSN: postgres:// or postgresql://
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		u, err := url.Parse(s)
		if err != nil {
			return dsn
		}
		q := u.Query()
		if keepsStricterSSLMode(q.Get("sslmode"), mode) {
			return dsn
		}
		q.Set("sslmode", mode)
		u.RawQuery = q.Encode()
		return u.String()
	}

	// Keyword/libpq DSN: replace or append sslmode
	parts := strings.Fields(s)
	for i, p := range parts {
		if current, ok := strings.CutPrefix(strings.ToLower(p), "sslmode="); ok {
			if !keepsStricterSSLMode(current, mode) {
				parts[i] = "sslmode=" + mode
			}
			return strings.Join(parts, " ")
		}
	}
	return strings.Join(append(parts, "sslmode="+mode), " ")
}
