// Package testbypass holds the switches that turn off or redirect security controls so the gateway can run against
// local mocks in development and in the E2E suite: DEV_MODE, E2E_TEST_MODE, COOKIE_SECURE=false, the AWS and
// Postmark endpoint overrides, and a GOOGLE_OAUTH_BASE_URL that isn't https.
//
// They are compiled in only with the testbypass build tag (enabled.go), which the development and E2E builds use.
// Release builds, including the published Docker image, are compiled without it (disabled.go): every bypass reads
// as off, so the binary can't honor them, and Check refuses to start while any of them is set. Code reads bypass
// state only through this package.
package testbypass

import (
	"strconv"
	"strings"
)

// flags turn a bypass on when set to a true value. endpoints redirect a service the gateway trusts when set to
// anything: audit anchors, the AWS credential exchange and key operations, or email along with the Postmark token.
// AWS_ENDPOINT_URL redirects every AWS service the SDK calls, so it's refused along with the per-service overrides.
var (
	flags     = []string{"DEV_MODE", "E2E_TEST_MODE"}
	endpoints = []string{
		"AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL_STS", "AWS_ENDPOINT_URL_KMS", "POSTMARK_API_BASE",
	}
)

// Active lists the bypasses set in the environment. A release build refuses to start with any of them (Check), and
// a testbypass build refuses them against a database marked production. getenv is os.Getenv outside tests.
func Active(getenv func(string) string) []string {
	var active []string
	for _, name := range flags {
		if flagOn(getenv(name)) {
			active = append(active, name)
		}
	}
	if secure, ok := parseBool(getenv("COOKIE_SECURE")); ok && !secure {
		active = append(active, "COOKIE_SECURE=false")
	}
	for _, name := range endpoints {
		if strings.TrimSpace(getenv(name)) != "" {
			active = append(active, name)
		}
	}
	if base := strings.TrimSpace(getenv("GOOGLE_OAUTH_BASE_URL")); base != "" && !strings.HasPrefix(base, "https://") {
		active = append(active, "GOOGLE_OAUTH_BASE_URL (must be https)")
	}
	return active
}

func flagOn(value string) bool {
	on, ok := parseBool(value)
	return ok && on
}

// parseBool parses value like strconv.ParseBool, and reports whether it's a boolean at all.
func parseBool(value string) (parsed, ok bool) {
	b, err := strconv.ParseBool(strings.TrimSpace(value))
	return b, err == nil
}
