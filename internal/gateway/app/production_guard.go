package app

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

// checkProductionSafety refuses to start a gateway whose database is marked production while any test bypass is
// set. A release build has already refused them at startup (testbypass.Check); this guards testbypass builds.
// getenv is os.Getenv in production; tests pass a fake.
func checkProductionSafety(dbEnvironment string, getenv func(string) string) error {
	if dbEnvironment != "production" {
		return nil
	}
	if active := testbypass.Active(getenv); len(active) > 0 {
		return fmt.Errorf(
			"refusing to start: the database is marked production but test bypasses are set: %s",
			strings.Join(active, ", "),
		)
	}
	return nil
}
