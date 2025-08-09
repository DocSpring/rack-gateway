package main

import (
	"os"
	"testing"

	"github.com/DocSpring/convox-gateway/internal/testutil/convoxguard"
)

func TestMain(m *testing.M) {
	cleanup, err := convoxguard.Setup()
	if err != nil {
		// fail fast if we can't protect prod config
		panic(err)
	}
	code := m.Run()
	if err := cleanup(); err != nil {
		// CRITICAL: Failed to restore config - this is more important than test results
		panic("CRITICAL: " + err.Error())
	}
	os.Exit(code)
}
