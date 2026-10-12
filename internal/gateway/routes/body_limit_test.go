package routes_test

import (
	"net/http"
	"strings"
	"testing"
)

// The signed-in API caps request bodies at 1 MiB before authentication, so even an unauthenticated caller can't make
// the gateway read a larger one. The largest real body, the web env editor's changes, is far smaller.
func TestSignedInAPIRefusesABodyOverOneMiB(t *testing.T) {
	e := newMFARouteEnv(t, "")

	over := e.do(http.MethodPut, "/api/v1/apps/myapp/env", strings.Repeat("a", 1<<20+1), nil)
	assertStatus(t, over, http.StatusRequestEntityTooLarge, "body over the limit")

	atLimit := e.do(http.MethodPut, "/api/v1/apps/myapp/env", strings.Repeat("a", 1<<20), nil)
	assertStatus(t, atLimit, http.StatusUnauthorized, "body at the limit reaches authentication")
}
