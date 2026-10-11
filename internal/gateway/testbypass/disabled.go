//go:build !testbypass

package testbypass

import (
	"fmt"
	"strings"
)

// Compiled is false: this is a release build, and every bypass reads as off.
const Compiled = false

// DevMode is always false in a release build.
func DevMode() bool { return false }

// E2ETestMode is always false in a release build: WebAuthn assertions are always verified.
func E2ETestMode() bool { return false }

// InsecureCookies is always false in a release build: cookies always have the Secure attribute.
func InsecureCookies(bool) bool { return false }

// S3EndpointOverride is always false in a release build: audit anchors always go to AWS S3.
func S3EndpointOverride() bool { return false }

// PostmarkAPIBase is always "" in a release build: email always goes to the Postmark API.
func PostmarkAPIBase() string { return "" }

// Check refuses to start a release build while any bypass is set, naming each one, so a deployment that expects
// one to take effect fails loudly instead of running without it.
func Check(getenv func(string) string) error {
	if active := Active(getenv); len(active) > 0 {
		return fmt.Errorf(
			"refusing to start: this build doesn't support test bypasses (it was built without the testbypass tag), "+
				"but these are set: %s",
			strings.Join(active, ", "),
		)
	}
	return nil
}
