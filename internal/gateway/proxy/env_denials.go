package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// envChangeDeniedError is an env change the caller may not make. The message names the refused keys and says
// why, and is returned to the client with a 403.
type envChangeDeniedError struct {
	message string
}

func (e *envChangeDeniedError) Error() string { return e.message }

// protectedEnvMFAError refuses a protected env change because MFA wasn't verified on the request. It is
// returned to the client with a 401 and X-MFA-Required, like other MFA refusals.
type protectedEnvMFAError struct {
	message string
}

func (e *protectedEnvMFAError) Error() string { return e.message }

func secretsDenied(keys []string) error {
	return &envChangeDeniedError{message: "You don't have permission to modify secrets: " + strings.Join(keys, ", ")}
}

// protectedKeysDenied refuses changes to protected env vars the request didn't explicitly opt in to. Protection
// works like deletion protection: nobody changes a protected key by accident, only an admin who names it with
// --allow-protected and verifies MFA, or after unprotecting it.
func protectedKeysDenied(app string, keys []string) error {
	if len(keys) == 1 {
		return &envChangeDeniedError{message: fmt.Sprintf(
			"%s is a protected env var for %s. An admin can change it with --allow-protected and MFA, "+
				"or unprotect it in rack-gateway settings.", keys[0], app,
		)}
	}
	return &envChangeDeniedError{message: fmt.Sprintf(
		"%s are protected env vars for %s. An admin can change them with --allow-protected and MFA, "+
			"or unprotect them in rack-gateway settings.",
		strings.Join(keys, ", "), app,
	)}
}

// writeEnvChangeRefusal writes the response for a refused env change and reports whether err was one.
func writeEnvChangeRefusal(w http.ResponseWriter, err error) bool {
	var denied *envChangeDeniedError
	if errors.As(err, &denied) {
		http.Error(w, denied.Error(), http.StatusForbidden)
		return true
	}
	var mfaRequired *protectedEnvMFAError
	if errors.As(err, &mfaRequired) {
		w.Header().Set("X-MFA-Required", "true")
		w.Header().Set("X-MFA-Level", rbac.MFAAlways.String())
		http.Error(w, mfaRequired.Error(), http.StatusUnauthorized)
		return true
	}
	return false
}
