// Package textcheck finds characters that make text read differently from what it is: control characters,
// invisible formatting (zero-width spaces and joiners, bidirectional overrides that reorder what follows, as in
// "Trojan Source" attacks) and line or paragraph separators.
package textcheck

import "unicode"

// Hidden reports whether r doesn't show as itself where people read text: a control character other than tab and
// line feed, a format character (Unicode category Cf: zero-width and bidirectional controls), or a line or
// paragraph separator.
func Hidden(r rune) bool {
	if r == '\t' || r == '\n' {
		return false
	}
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.In(r, unicode.Zl, unicode.Zp)
}
