package scriptreview

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/DocSpring/rack-gateway/internal/scriptrun"
	"github.com/DocSpring/rack-gateway/internal/util/textcheck"
)

// span is a piece of a script's listing and how it's shown: one of the style names, or "" for plain text.
type span struct {
	Text  string `json:"t"`
	Style string `json:"s,omitempty"`
}

// The styles of a listing. The dialog and the terminal each give them their own colors.
const (
	styleLineNumber = "line"
	styleHidden     = "hidden"
	styleComment    = "comment"
	styleKeyword    = "keyword"
	styleString     = "string"
	styleSymbol     = "symbol"
	styleNumber     = "number"
	styleType       = "type"
	styleFunction   = "function"
)

// tokenizeTimeout bounds how long highlighting may take. The lexers use backtracking regular expressions, so an
// unusual script could take a long time; it's then shown without highlighting.
const tokenizeTimeout = 2 * time.Second

// listing returns the script as it's shown for review: numbered lines, highlighted for the script's language, with
// every character that wouldn't show as itself (see textcheck.Hidden) written as <U+XXXX>, and how many there
// were. CRLF line endings count as line breaks.
func listing(script scriptrun.Script) ([]span, int) {
	text := strings.TrimSuffix(strings.ReplaceAll(script.Text, "\r\n", "\n"), "\n")
	out := &listingWriter{width: len(strconv.Itoa(strings.Count(text, "\n") + 1))}
	out.lineNumber()
	for _, token := range tokenize(scriptLexer(script), text) {
		for i, part := range strings.Split(token.Text, "\n") {
			if i > 0 {
				out.add("\n", "")
				out.lineNumber()
			}
			out.addVisible(part, token.Style)
		}
	}
	return out.spans, out.hidden
}

// plainText is a listing's text without its styles.
func plainText(spans []span) string {
	var out strings.Builder
	for _, s := range spans {
		out.WriteString(s.Text)
	}
	return out.String()
}

// listingWriter builds a listing, joining adjacent spans of the same style.
type listingWriter struct {
	spans               []span
	width, line, hidden int
}

func (l *listingWriter) add(text, style string) {
	if text == "" {
		return
	}
	if n := len(l.spans); n > 0 && l.spans[n-1].Style == style {
		l.spans[n-1].Text += text
		return
	}
	l.spans = append(l.spans, span{Text: text, Style: style})
}

func (l *listingWriter) lineNumber() {
	l.line++
	l.add(fmt.Sprintf("%*d | ", l.width, l.line), styleLineNumber)
}

// addVisible adds text in style, with each hidden character written as <U+XXXX>.
func (l *listingWriter) addVisible(text, style string) {
	start := 0
	for i, r := range text {
		if !textcheck.Hidden(r) {
			continue
		}
		l.add(text[start:i], style)
		l.add(fmt.Sprintf("<U+%04X>", r), styleHidden)
		l.hidden++
		start = i + utf8.RuneLen(r)
	}
	l.add(text[start:], style)
}

// hiddenCount is how many characters of text its listing writes as <U+XXXX>.
func hiddenCount(text string) int {
	count := 0
	for _, r := range strings.ReplaceAll(text, "\r\n", "\n") {
		if textcheck.Hidden(r) {
			count++
		}
	}
	return count
}

// commandLanguages are the languages of scripts run by these programs, for a script whose file name doesn't say.
var commandLanguages = map[string]string{
	"rails": "ruby", "ruby": "ruby", "irb": "ruby",
	"python": "python", "python3": "python",
	"node": "javascript",
	"sh":   "bash", "bash": "bash",
	"psql": "postgresql",
}

// scriptLexer is the lexer for the script's language: from its file name, else from the program that runs it
// (`rails runner`, `bundle exec ruby`), else nil.
func scriptLexer(script scriptrun.Script) chroma.Lexer {
	if lexer := lexers.Match(script.Name); lexer != nil {
		return lexer
	}
	words := strings.Fields(script.Words)
	if len(words) >= 2 && words[0] == "bundle" && words[1] == "exec" {
		words = words[2:]
	}
	if len(words) == 0 {
		return nil
	}
	language, ok := commandLanguages[path.Base(words[0])]
	if !ok {
		return nil
	}
	return lexers.Get(language)
}

// tokenize splits text into spans styled by lexer. Text without a lexer, or that its lexer fails on or takes too
// long with, is one plain span.
func tokenize(lexer chroma.Lexer, text string) []span {
	plain := []span{{Text: text}}
	if lexer == nil {
		return plain
	}
	result := make(chan []span, 1)
	go func() {
		var spans []span
		defer func() {
			_ = recover() // a lexer that fails leaves the script plain
			result <- spans
		}()
		spans = lexSpans(chroma.Coalesce(lexer), text)
	}()
	select {
	case spans := <-result:
		if spans == nil {
			return plain
		}
		return spans
	case <-time.After(tokenizeTimeout):
		return plain
	}
}

// lexSpans tokenizes text with lexer. It returns nil when the lexer fails or its tokens aren't text exactly (some
// lexers add a final line break, which is dropped).
func lexSpans(lexer chroma.Lexer, text string) []span {
	tokens, err := chroma.Tokenise(lexer, nil, text)
	if err != nil {
		return nil
	}
	spans := make([]span, 0, len(tokens))
	for _, token := range tokens {
		spans = append(spans, span{Text: token.Value, Style: tokenStyle(token.Type)})
	}
	if n := len(spans); n > 0 && plainText(spans) == text+"\n" {
		spans[n-1].Text = strings.TrimSuffix(spans[n-1].Text, "\n")
	}
	if plainText(spans) != text {
		return nil
	}
	return spans
}

// tokenStyle is the style of a kind of token.
func tokenStyle(t chroma.TokenType) string {
	switch {
	case t.InCategory(chroma.Comment):
		return styleComment
	case t.InCategory(chroma.Keyword):
		return styleKeyword
	case t == chroma.LiteralStringSymbol:
		return styleSymbol
	case t.InSubCategory(chroma.LiteralString):
		return styleString
	case t.InCategory(chroma.Literal):
		return styleNumber
	case t == chroma.NameClass, t == chroma.NameConstant, t == chroma.NameNamespace, t == chroma.NameException:
		return styleType
	case t.InSubCategory(chroma.NameFunction), t.InSubCategory(chroma.NameBuiltin), t == chroma.NameDecorator:
		return styleFunction
	default:
		return ""
	}
}

// ansiStyles are the terminal colors of a listing's styles.
var ansiStyles = map[string]string{
	styleLineNumber: "90",
	styleHidden:     "1;97;41",
	styleComment:    "90",
	styleKeyword:    "35",
	styleString:     "33",
	styleSymbol:     "32",
	styleNumber:     "95",
	styleType:       "36",
	styleFunction:   "34",
}

// ansiText is a listing in terminal colors. No styled span holds a line break, so a color never runs past the end
// of a line, and the script can't add escape sequences of its own: those are hidden characters.
func ansiText(spans []span) string {
	var out strings.Builder
	for _, s := range spans {
		code, ok := ansiStyles[s.Style]
		if !ok {
			out.WriteString(s.Text)
			continue
		}
		out.WriteString("\x1b[" + code + "m" + s.Text + "\x1b[0m")
	}
	return out.String()
}
