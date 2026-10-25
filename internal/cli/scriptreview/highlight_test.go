package scriptreview

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// styleOf returns the style of the span holding text, failing when no single span does.
func styleOf(t *testing.T, spans []span, text string) string {
	t.Helper()
	for _, s := range spans {
		if strings.Contains(s.Text, text) {
			return s.Style
		}
	}
	require.Failf(t, "no span holds the text", "%q in %v", text, spans)
	return ""
}

// A Ruby script is highlighted by kind of token, and its text is unchanged.
func TestListingHighlightsTheScriptsLanguage(t *testing.T) {
	script := "# Count accounts\naccounts = Account.where(plan: 'pro', state: :active)\nputs accounts.count + 42\n"
	spans, hidden := listing(scriptrun.Script{Name: "count.rb", Text: script})
	require.Zero(t, hidden)
	require.Equal(t, "1 | # Count accounts\n2 | accounts = Account.where(plan: 'pro', state: :active)\n"+
		"3 | puts accounts.count + 42", plainText(spans))
	require.Equal(t, styleLineNumber, styleOf(t, spans, "2 | "))
	require.Equal(t, styleComment, styleOf(t, spans, "# Count accounts"))
	require.Equal(t, styleType, styleOf(t, spans, "Account"))
	require.Equal(t, styleString, styleOf(t, spans, "'pro'"))
	require.Equal(t, styleSymbol, styleOf(t, spans, ":active"))
	require.Equal(t, styleFunction, styleOf(t, spans, "puts"))
	require.Equal(t, styleNumber, styleOf(t, spans, "42"))
	for _, s := range spans {
		if s.Style != "" {
			require.NotContains(t, s.Text, "\n", "a styled span never holds a line break")
		}
	}
}

// A script whose file name doesn't say its language gets the language of the program that runs it.
func TestListingLanguageFromTheCommand(t *testing.T) {
	for _, words := range []string{"rails runner", "bundle exec rails runner", "bin/rails runner", "ruby"} {
		spans, _ := listing(scriptrun.Script{Name: "check", Text: "puts Account.count\n", Words: words})
		require.Equal(t, styleType, styleOf(t, spans, "Account"), words)
	}
}

// Without a known language (no file extension, a program it doesn't know) the listing is plain text.
func TestListingOfUnknownLanguageIsPlain(t *testing.T) {
	spans, _ := listing(scriptrun.Script{Name: "notes", Text: "first line\nsecond line\n", Words: "cat"})
	require.Equal(t, []span{
		{Text: "1 | ", Style: styleLineNumber},
		{Text: "first line\n"},
		{Text: "2 | ", Style: styleLineNumber},
		{Text: "second line"},
	}, spans)
}

// In a terminal, styled spans are wrapped in ANSI colors, each ending with a reset. An escape character in the
// script is a hidden character, written out, so the script can't color or move the terminal itself.
func TestANSITextColorsSpans(t *testing.T) {
	spans, hidden := listing(scriptrun.Script{Name: "x.rb", Text: "puts 1 # \x1b[2Jdone\n"})
	require.Equal(t, 1, hidden)
	text := ansiText(spans)
	require.Contains(t, text, "\x1b[90m1 | \x1b[0m")
	require.Contains(t, text, "\x1b[1;97;41m<U+001B>\x1b[0m")
	require.Equal(t, 2*strings.Count(text, "\x1b[0m"), strings.Count(text, "\x1b["),
		"the only escape sequences are the listing's own colors and resets")
}

// Every language a program maps to has a lexer.
func TestCommandLanguagesHaveLexers(t *testing.T) {
	for program, language := range commandLanguages {
		require.NotNil(t, lexers.Get(language), "%s: %s", program, language)
	}
}
