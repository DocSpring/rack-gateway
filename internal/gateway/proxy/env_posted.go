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
// over the limits is refused before it is parsed, and so is any entry envutil.ValidateEnvEntry refuses: the
// rack would store it, and `convox env` would print it to other users' terminals.
func parsePostedEnv(envStr string) (map[string]string, []string, error) {
	if err := checkPostedEnvSize(envStr); err != nil {
		return nil, nil, err
	}
	posted := make(map[string]string)
	var order []string
	// Cut walks the lines without building a slice of them, and every key and value is a substring of envStr.
	for rest := envStr; rest != ""; {
		var line string
		line, rest, _ = strings.Cut(rest, "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		rawKey, val, _ := strings.Cut(line, "=")
		key := strings.TrimSpace(rawKey)
		if err := envutil.ValidateEnvEntry(key, val); err != nil {
			return nil, nil, err
		}
		if _, seen := posted[key]; !seen {
			order = append(order, key)
		}
		posted[key] = val
	}
	return posted, order, nil
}

// checkPostedEnvSize refuses an env over maxPostedEnvBytes or maxPostedEnvLines, counting blank lines too.
func checkPostedEnvSize(envStr string) error {
	if len(envStr) > maxPostedEnvBytes {
		return &postedEnvTooLargeError{
			message: fmt.Sprintf("the release env is over the gateway's limit of %d bytes", maxPostedEnvBytes),
		}
	}
	if strings.Count(envStr, "\n") >= maxPostedEnvLines {
		return &postedEnvTooLargeError{
			message: fmt.Sprintf("the release env is over the gateway's limit of %d lines", maxPostedEnvLines),
		}
	}
	return nil
}
