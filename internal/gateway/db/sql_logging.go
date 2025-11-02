package db

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

func formatArgs(args []interface{}) string {
	if len(args) == 0 {
		return "[]"
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = fmt.Sprintf("%v", arg)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func sqlColor(query string) string {
	upper := strings.ToUpper(strings.TrimSpace(query))
	switch {
	case strings.HasPrefix(upper, "SELECT"):
		return colorCyan
	case strings.HasPrefix(upper, "INSERT"):
		return colorGreen
	case strings.HasPrefix(upper, "UPDATE"):
		return colorYellow
	case strings.HasPrefix(upper, "DELETE"):
		return colorRed
	default:
		return colorMagenta
	}
}

func queryCaller() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function == "" {
			if !more {
				break
			}
			continue
		}
		file := frame.File
		if !strings.Contains(file, "/internal/gateway/db/") && !strings.Contains(file, "\\internal\\gateway\\db\\") {
			rel := relativePath(file)
			return fmt.Sprintf("%s:%d", rel, frame.Line)
		}
		if !more {
			break
		}
	}
	return ""
}

func logSQLTrace() {
	if !gtwlog.TopicEnabled(gtwlog.TopicSQLTrace) {
		return
	}

	lines := collectTraceLines()
	if len(lines) == 0 {
		return
	}

	gtwlog.DebugTopicf(gtwlog.TopicSQLTrace, "%s", strings.Join(lines, "\n"))
}

func collectTraceLines() []string {
	const traceDepth = 10
	pcs := make([]uintptr, 32)
	n := runtime.Callers(4, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	lines := make([]string, 0, traceDepth)
	depth := 0

	for {
		frame, more := frames.Next()
		if frame.Function == "" {
			if !more {
				break
			}
			continue
		}

		if shouldSkipFrame(frame.File) {
			if !more {
				break
			}
			continue
		}

		rel := relativePath(frame.File)
		lines = append(lines, fmt.Sprintf("#%d %s (%s:%d)", depth, frame.Function, rel, frame.Line))
		depth++
		if depth >= traceDepth || !more {
			break
		}
	}

	return lines
}

func shouldSkipFrame(file string) bool {
	// Skip internal database frames so trace points to caller sites
	if strings.Contains(file, "/internal/gateway/db/") || strings.Contains(file, "\\internal\\gateway\\db\\") {
		return true
	}
	// Skip Go runtime frames
	if strings.Contains(file, "/src/runtime/") {
		return true
	}
	return false
}

func relativePath(file string) string {
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Base(file)
	}
	rel, err := filepath.Rel(wd, file)
	if err != nil {
		return filepath.Base(file)
	}
	return rel
}

func validateResetPermission() error {
	if os.Getenv("RESET_RACK_GATEWAY_DATABASE") != "DELETE_ALL_DATA" {
		return fmt.Errorf(
			"refusing to reset database: set RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA to proceed",
		)
	}
	return nil
}

const (
	colorReset   = "\033[0m"
	colorGray    = "\033[90m"
	colorCyan    = "\033[36m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorRed     = "\033[31m"
	colorMagenta = "\033[35m"
)
