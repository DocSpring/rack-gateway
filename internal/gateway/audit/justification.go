package audit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/DocSpring/rack-gateway/internal/util/textcheck"
)

// JustificationHeader carries a command's justification (an exec's, or a process start's) from the CLI to the
// gateway, percent-encoded UTF-8. The gateway records it with the command and doesn't forward it to the rack.
const JustificationHeader = "X-Command-Justification"

// A justification's length, in characters: long enough to say why a command is run and what it does, short enough
// for one line of a Slack message.
const (
	MinJustificationChars = 16
	MaxJustificationChars = 1000
)

// CheckJustification refuses a justification that can't say why a command is run and what it does: fewer than
// MinJustificationChars characters (spaces at the ends aside), more than MaxJustificationChars, more than one line,
// or with characters that read as something else (see textcheck.Hidden).
func CheckJustification(text string) error {
	if !utf8.ValidString(text) {
		return errors.New("the justification isn't UTF-8 text")
	}
	for _, r := range text {
		if r == '\n' || r == '\t' || textcheck.Hidden(r) {
			return fmt.Errorf("the justification must be one line of plain text (it has %U)", r)
		}
	}
	length := utf8.RuneCountInString(strings.TrimSpace(text))
	if length < MinJustificationChars {
		return fmt.Errorf("the justification is too short (%d characters, at least %d): say why the command is run "+
			"and what it does", length, MinJustificationChars)
	}
	if length > MaxJustificationChars {
		return fmt.Errorf("the justification is too long (%d characters, at most %d)", length, MaxJustificationChars)
	}
	return nil
}

// EncodeJustification returns text as JustificationHeader carries it.
func EncodeJustification(text string) string {
	return url.QueryEscape(strings.TrimSpace(text))
}

// RequestJustification returns the justification r carries in JustificationHeader, decoded and checked, or ""
// when it carries none.
func RequestJustification(r *http.Request) (string, error) {
	values := r.Header.Values(JustificationHeader)
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > 1 {
		return "", errors.New("the request carries more than one justification")
	}
	text, err := url.QueryUnescape(values[0])
	if err != nil {
		return "", fmt.Errorf("the justification isn't percent-encoded: %w", err)
	}
	if err := CheckJustification(text); err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

type justificationContextKey struct{}

// WithJustification returns ctx carrying the justification of the command its request runs, which the request's
// audit entries record (see BuildDetailsJSON).
func WithJustification(ctx context.Context, text string) context.Context {
	return context.WithValue(ctx, justificationContextKey{}, text)
}

// JustificationFromContext returns the justification WithJustification gave ctx, or "".
func JustificationFromContext(ctx context.Context) string {
	text, _ := ctx.Value(justificationContextKey{}).(string)
	return text
}
