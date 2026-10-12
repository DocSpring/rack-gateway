// Package debuglog turns on debug logging topics for a test and captures what's logged, so a test can check that
// a debug log carries no secrets.
package debuglog

import (
	"bytes"
	"log"
	"os"
	"testing"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Capture turns on topics (a DEBUG_TOPICS value, such as "http.*") at debug level for the rest of the test and
// returns what gets logged. The test must not be parallel: the logger and log output are global.
func Capture(t *testing.T, topics string) *bytes.Buffer {
	t.Helper()
	// Cleanups run last-registered first, so each env var is restored before the logger re-reads the env. LOG_LEVEL
	// is set to info first: the logger keeps its level when LOG_LEVEL is unset, so it must re-read info before the
	// original (usually unset) LOG_LEVEL comes back, or later tests would log at debug.
	t.Cleanup(gtwlog.Reload)
	t.Setenv("LOG_LEVEL", "info")
	t.Cleanup(gtwlog.Reload)
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DEBUG_TOPICS", topics)
	gtwlog.Reload()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &logs
}
