package logutil

import (
	"strings"
	"unicode"
)

// SanitizeForLog removes control characters to prevent log injection.
func SanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, s)
}
