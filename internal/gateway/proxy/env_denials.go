package proxy

import (
	"fmt"
	"strings"
)

// envChangeDeniedError is an env change the caller may not make. The message names the refused keys and says
// why, and is returned to the client with a 403.
type envChangeDeniedError struct {
	message string
}

func (e *envChangeDeniedError) Error() string { return e.message }

func secretsDenied(keys []string) error {
	return &envChangeDeniedError{message: "You don't have permission to modify secrets: " + strings.Join(keys, ", ")}
}

// protectedKeysDenied refuses changes to protected env vars. Protection works like deletion protection: an
// admin has to unprotect the key before anyone can change or remove it.
func protectedKeysDenied(app string, keys []string) error {
	if len(keys) == 1 {
		return &envChangeDeniedError{message: fmt.Sprintf(
			"%s is a protected env var for %s. Unprotect it in rack-gateway settings to change it.", keys[0], app,
		)}
	}
	return &envChangeDeniedError{message: fmt.Sprintf(
		"%s are protected env vars for %s. Unprotect them in rack-gateway settings to change them.",
		strings.Join(keys, ", "), app,
	)}
}
