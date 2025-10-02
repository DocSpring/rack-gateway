package rbac

import (
	"os"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/convoxguard"
)

func TestMain(m *testing.M) {
	cleanup, err := convoxguard.Setup()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	if err := cleanup(); err != nil {
		panic("CRITICAL: " + err.Error())
	}
	os.Exit(code)
}
