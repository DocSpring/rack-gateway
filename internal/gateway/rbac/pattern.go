package rbac

import (
	"regexp"
	"strings"
)

// KeyMatch3 simplified: supports {var} placeholders and wildcards, shared by proxies and audit logic.
func KeyMatch3(path, pattern string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '{' {
			for i < len(pattern) && pattern[i] != '}' {
				i++
			}
			b.WriteString("[^/]+")
			continue
		}
		if c == '*' {
			b.WriteString(".*")
			continue
		}
		if strings.ContainsRune(".+?^$()[]{}|\\", rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteString("$")
	re := b.String()
	ok, _ := regexp.MatchString(re, path)
	return ok
}
