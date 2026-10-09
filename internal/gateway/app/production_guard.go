package app

import (
	"fmt"
	"strconv"
	"strings"
)

// testOnlyFlags and testOnlyEndpoints switch off or redirect security controls for the development and
// E2E stacks (E2E_TEST_MODE skips WebAuthn assertion checks, DEV_MODE relaxes cookies/CSP/secrets, and the
// endpoint overrides send audit anchors and the Postmark token elsewhere). They must never be active
// against a database marked as production.
var (
	testOnlyFlags     = []string{"DEV_MODE", "E2E_TEST_MODE"}
	testOnlyEndpoints = []string{"AWS_ENDPOINT_URL_S3", "POSTMARK_API_BASE"}
)

// checkProductionSafety refuses to start a gateway whose database is marked production while any
// test-only switch is set. getenv is os.Getenv in production; tests pass a fake.
func checkProductionSafety(dbEnvironment string, getenv func(string) string) error {
	if dbEnvironment != "production" {
		return nil
	}
	var offending []string
	for _, name := range testOnlyFlags {
		if enabled, err := strconv.ParseBool(strings.TrimSpace(getenv(name))); err == nil && enabled {
			offending = append(offending, name)
		}
	}
	for _, name := range testOnlyEndpoints {
		if strings.TrimSpace(getenv(name)) != "" {
			offending = append(offending, name)
		}
	}
	if base := strings.TrimSpace(getenv("GOOGLE_OAUTH_BASE_URL")); base != "" && !strings.HasPrefix(base, "https://") {
		offending = append(offending, "GOOGLE_OAUTH_BASE_URL (must be https)")
	}
	if len(offending) > 0 {
		return fmt.Errorf(
			"refusing to start: the database is marked production but test-only settings are set: %s",
			strings.Join(offending, ", "),
		)
	}
	return nil
}
