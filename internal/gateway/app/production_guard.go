package app

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

// checkProductionSafety refuses to start a gateway whose database is marked production while any test bypass is
// set, or while COLUMN_ENCRYPTION_KEY is the development key published in this repository. A release build has
// already refused the bypasses at startup (testbypass.Check), so for them this guards testbypass builds; the
// development key is refused in every build. getenv is os.Getenv in production; tests pass a fake.
func checkProductionSafety(dbEnvironment string, getenv func(string) string) error {
	if dbEnvironment != "production" {
		return nil
	}
	refused := testbypass.Active(getenv)
	if strings.TrimSpace(getenv(colcrypt.EnvVar)) == colcrypt.DevKey {
		refused = append(refused, colcrypt.EnvVar+" (the published development key)")
	}
	if len(refused) > 0 {
		return fmt.Errorf(
			"refusing to start: the database is marked production but test settings are set: %s",
			strings.Join(refused, ", "),
		)
	}
	return nil
}
