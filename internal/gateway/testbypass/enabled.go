//go:build testbypass

package testbypass

import (
	"os"
	"strings"
)

// Compiled is true: this build honors the test bypasses. Never deploy it.
const Compiled = true

// DevMode reports DEV_MODE: optional secrets and allowed domain, a local dev rack, migrations on startup, relaxed
// CSP, CORS, origin checks and cookies, and emails logged instead of dropped when Postmark isn't configured.
func DevMode() bool {
	return flagOn(os.Getenv("DEV_MODE"))
}

// E2ETestMode reports E2E_TEST_MODE: WebAuthn assertions pass without verification, because the browser tests can't
// hold a real authenticator, and the web UI is told it's under test.
func E2ETestMode() bool {
	return flagOn(os.Getenv("E2E_TEST_MODE"))
}

// InsecureCookies reports whether cookies leave out the Secure attribute, for stacks served over plain HTTP:
// COOKIE_SECURE=false, or dev mode without COOKIE_SECURE=true.
func InsecureCookies(devMode bool) bool {
	if secure, ok := parseBool(os.Getenv("COOKIE_SECURE")); ok {
		return !secure
	}
	return devMode
}

// S3EndpointOverride reports whether AWS_ENDPOINT_URL_S3 sends audit anchors to another S3 endpoint (MinIO), which
// needs path-style addressing and doesn't support SSE-KMS or checksums.
func S3EndpointOverride() bool {
	return strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL_S3")) != ""
}

// PostmarkAPIBase returns POSTMARK_API_BASE, which sends email and the Postmark token to a mock server, or "".
func PostmarkAPIBase() string {
	return strings.TrimSpace(os.Getenv("POSTMARK_API_BASE"))
}

// Check allows every bypass in a testbypass build. The app still refuses them against a database marked production.
func Check(func(string) string) error {
	return nil
}
