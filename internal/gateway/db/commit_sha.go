package db

import (
	"regexp"
	"strings"
)

var fullCommitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// NormalizeCommitSHA trims and lowercases a git commit hash and reports whether it is a
// full 40-character SHA-1. Deploy approvals are bound to full commits only.
func NormalizeCommitSHA(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized, fullCommitSHAPattern.MatchString(normalized)
}

// likePrefixPattern escapes LIKE wildcards in prefix and appends % so it only matches values
// that start with the literal prefix. Use with "LIKE ? ESCAPE '\'".
func likePrefixPattern(prefix string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	return escaped + "%"
}
