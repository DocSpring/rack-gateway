package proxy

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// Limits on the env a release create posts. It is parsed before any env permission check, so parsing it must stay
// cheap whatever the caller sends: the body may be up to 10 MB, and an env of millions of blank lines would cost
// far more memory than its bytes. A real app's env is a few hundred lines and tens of KB, well inside both.
const (
	maxPostedEnvBytes = 1 << 20 // 1 MiB
	maxPostedEnvLines = 10000
)

// postedEnvTooLargeError refuses an env over the limits. It counts as errBodyTooLarge, so the client gets a 413.
type postedEnvTooLargeError struct {
	message string
}

func (e *postedEnvTooLargeError) Error() string { return e.message }

func (*postedEnvTooLargeError) Is(target error) bool { return target == errBodyTooLarge }

// parsePostedEnv parses a posted env of KEY=VALUE lines (a line without = sets KEY to ""). It returns the values,
// and the keys in the order first posted; a repeated key takes its last value. Blank lines are skipped. An env
// over the limits is refused before it is parsed, and so is a key with a control character: the permission checks
// name keys in audit entries before the current env is fetched. validateChangedEntries checks the rest of each
// entry once it is.
func parsePostedEnv(envStr string) (map[string]string, []string, error) {
	if err := checkPostedEnvSize(envStr); err != nil {
		return nil, nil, err
	}
	posted := make(map[string]string)
	var order []string
	err := eachPostedEntry(envStr, func(line int, key, value string) error {
		if envutil.HasControlCharacter(key) {
			return fmt.Errorf("line %d: %w: a name contains a control character or isn't UTF-8",
				line, envutil.ErrInvalidEnvEntry)
		}
		if _, seen := posted[key]; !seen {
			order = append(order, key)
		}
		posted[key] = value
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return posted, order, nil
}

// validateChangedEntries refuses a posted entry that is new, or changes its key's value, when
// envutil.ValidateEnvEntry refuses it: the rack would store it, and `convox env` would print it to other users'
// terminals. An entry the current env already has is left alone, so a key or value the gateway wouldn't accept
// today (`spring.profiles.active`, say) doesn't block changes to other keys. A masked value keeps the current one
// (mergeEnvAndComputeDiffs refuses it for a key without one). The error names the line rather than the key, which
// may be a line of a multi-line secret value.
func validateChangedEntries(envStr string, baseEnv map[string]string) error {
	return eachPostedEntry(envStr, func(line int, key, value string) error {
		if base, exists := baseEnv[key]; value == maskedSecret || (exists && value == base) {
			return nil
		}
		if err := envutil.ValidateEnvEntry(key, value); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		return nil
	})
}

// eachPostedEntry calls fn for each non-blank line of a posted env, with its line number (from 1), its key
// (trimmed) and its value, until fn returns an error. Cut walks the lines without building a slice of them, and
// every key and value is a substring of envStr.
func eachPostedEntry(envStr string, fn func(line int, key, value string) error) error {
	line := 0
	for rest := envStr; rest != ""; {
		var text string
		text, rest, _ = strings.Cut(rest, "\n")
		line++
		if strings.TrimSpace(text) == "" {
			continue
		}
		rawKey, value, _ := strings.Cut(text, "=")
		if err := fn(line, strings.TrimSpace(rawKey), value); err != nil {
			return err
		}
	}
	return nil
}

// checkPostedEnvSize refuses an env over maxPostedEnvBytes or maxPostedEnvLines, counting blank lines too.
func checkPostedEnvSize(envStr string) error {
	if len(envStr) > maxPostedEnvBytes {
		return &postedEnvTooLargeError{
			message: fmt.Sprintf("the release env is over the gateway's limit of %d bytes", maxPostedEnvBytes),
		}
	}
	if postedEnvLines(envStr) > maxPostedEnvLines {
		return &postedEnvTooLargeError{
			message: fmt.Sprintf("the release env is over the gateway's limit of %d lines", maxPostedEnvLines),
		}
	}
	return nil
}

// postedEnvLines counts the lines of a posted env. A newline at the end finishes the last line rather than
// starting another.
func postedEnvLines(envStr string) int {
	lines := strings.Count(envStr, "\n")
	if envStr != "" && !strings.HasSuffix(envStr, "\n") {
		lines++
	}
	return lines
}
