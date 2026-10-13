package audit

import (
	"os"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/convoxguard"
)

func TestMain(m *testing.M) {
	os.Exit(convoxguard.Run(m))
}
