package cli

import (
	"fmt"
	"strings"
	"unicode"
)

// displayText makes text from outside the CLI safe to print on one line: gateway responses (request IDs,
// statuses, error messages, emails) and CI-supplied request fields (messages, app and branch names). Control
// characters (newlines, terminal escape sequences, C1 controls), Unicode line and paragraph separators and
// bidirectional overrides become '?', so the text can't rewrite the terminal, add lines, or hide or reorder text
// from the person or agent reading it.
func displayText(s string) string {
	return strings.Map(func(r rune) rune {
		if isUnsafeDisplayRune(r) {
			return '?'
		}
		return r
	}, s)
}

// DisplayError renders an error for the terminal. Errors can quote gateway responses (including the ones the
// convox SDK wraps), so everything displayText replaces is replaced here too, except newlines and tabs: some
// errors span lines on purpose (cobra's "Did you mean this?" suggestions).
func DisplayError(err error) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && isUnsafeDisplayRune(r) {
			return '?'
		}
		return r
	}, err.Error())
}

// terminalSafeJSON escapes the characters that encoding/json leaves raw but a terminal acts on: DEL, C1 controls
// (U+009B is a one-character CSI) and bidirectional overrides. Each becomes a \uXXXX escape, which a JSON parser
// decodes back to the same string. encoding/json already escapes C0 controls, U+2028 and U+2029 inside strings,
// so the raw C0 characters left in data are its own indentation and are kept.
func terminalSafeJSON(data []byte) string {
	var out strings.Builder
	for _, r := range string(data) {
		if r >= unicode.MaxASCII && isUnsafeDisplayRune(r) {
			_, _ = fmt.Fprintf(&out, `\u%04x`, r)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func isUnsafeDisplayRune(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control, unicode.Zl, unicode.Zp)
}
