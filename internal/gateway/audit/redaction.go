package audit

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// redactedValue replaces a credential the audit log mustn't keep.
const redactedValue = "[REDACTED]"

// sensitiveQueryParams carry one-time login credentials or OAuth values and must never be logged.
var sensitiveQueryParams = map[string]struct{}{
	"code":          {},
	"state":         {},
	"login_code":    {},
	"code_verifier": {},
	"token":         {},
	"access_token":  {},
	"id_token":      {},
}

// RedactQuery returns rawQuery with the values of sensitive parameters replaced by [REDACTED].
func RedactQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[UNPARSEABLE]"
	}
	for key := range values {
		if _, sensitive := sensitiveQueryParams[strings.ToLower(key)]; sensitive {
			values[key] = []string{redactedValue}
		}
	}
	return values.Encode()
}

// commandURLUserinfo finds a URL's user information in a command (the user:password@ of
// postgres://user:password@host).
var commandURLUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@'"]+@`)

// commandWord finds the words of a command as a shell splits them: a quoted part, spaces and all, belongs to the
// word it's in. An unmatched quote is a word of its own.
var commandWord = regexp.MustCompile(`(?:"[^"]*"|'[^']*'|[^\s"'])+|["']`)

// commandHeader is a header as a command passes one (curl -H 'X-API-Key: value', or X-API-Key:value): its name,
// the colon and any space after it, and its value.
var commandHeader = regexp.MustCompile(`(?s)^([A-Za-z][A-Za-z0-9-]*)(:\s*)(\S.*)$`)

// credentialHeaders carry a credential, whatever their value looks like.
var credentialHeaders = map[string]bool{"authorization": true, "proxy-authorization": true, "cookie": true}

// RedactCommand returns an exec command as the audit log keeps it, with the credentials a command line commonly
// carries replaced by [REDACTED]: a URL's user information (postgres://user:password@host), the value of a
// secret-named assignment or flag (DB_PASSWORD=..., --api-key=..., --token ...; see envutil.IsSecretKey), a
// credential header's value (see redactHeader), and the word after Bearer or Basic. The rest is kept, so the record
// still says what was run. It can't recognize every credential (a password after mysql's -p, say).
func RedactCommand(command string) string {
	command = commandURLUserinfo.ReplaceAllString(command, "${1}"+redactedValue+"@")
	var out strings.Builder
	last, credentialNext := 0, false
	for _, span := range commandWord.FindAllStringIndex(command, -1) {
		out.WriteString(command[last:span[0]])
		word := command[span[0]:span[1]]
		last = span[1]
		if credentialNext {
			out.WriteString(redactQuoted(word))
			credentialNext = false
			continue
		}
		var redacted string
		redacted, credentialNext = redactCommandWord(word)
		out.WriteString(redacted)
	}
	out.WriteString(command[last:])
	return out.String()
}

// redactCommandWord redacts the value of a credential header or a secret-named assignment or flag (NAME=value), and
// reports whether the next word is a credential: one after a secret-named flag, or after Bearer or Basic. A quoted
// word with spaces in it (sh -c '...', "host=db password=...") is redacted as a command of its own.
func redactCommandWord(word string) (string, bool) {
	bare := strings.Trim(word, `"'`)
	if header, ok := redactHeader(bare); ok {
		return strings.Replace(word, bare, header, 1), false
	}
	if inner, ok := quotedPhrase(word); ok {
		return word[:1] + RedactCommand(inner) + word[len(word)-1:], false
	}
	if name, _, ok := strings.Cut(bare, "="); ok {
		if !isSecretName(name) {
			return word, false
		}
		assign := strings.Index(word, "=")
		return word[:assign+1] + redactQuoted(word[assign+1:]), false
	}
	if strings.HasPrefix(bare, "-") && isSecretName(bare) {
		return word, true
	}
	return word, strings.EqualFold(bare, "bearer") || strings.EqualFold(bare, "basic")
}

// redactHeader redacts a credential header's whole value in text, a word without its quotes, and reports whether it
// did. A credential header is Authorization, Proxy-Authorization or Cookie, or one with a secret's name (X-API-Key,
// X-Auth-Token; see isSecretName). Without a space after its colon, text is only taken for a header when its name
// has a hyphen, so a task such as credentials:edit isn't.
func redactHeader(text string) (string, bool) {
	match := commandHeader.FindStringSubmatch(text)
	if match == nil {
		return text, false
	}
	name, separator := match[1], match[2]
	if !credentialHeaders[strings.ToLower(name)] {
		headerLike := len(separator) > 1 || strings.Contains(name, "-")
		if !headerLike || !isSecretName(name) {
			return text, false
		}
	}
	return name + separator + redactedValue, true
}

// quotedPhrase returns what's inside word when it's one quoted part with whitespace in it.
func quotedPhrase(word string) (string, bool) {
	if len(word) < 2 || (word[0] != '"' && word[0] != '\'') || word[len(word)-1] != word[0] {
		return "", false
	}
	inner := word[1 : len(word)-1]
	return inner, !strings.ContainsRune(inner, rune(word[0])) && strings.ContainsAny(inner, " \t\n")
}

// redactQuoted replaces value with [REDACTED], keeping the quotes it ends with (the word may close a quoted
// argument) or starts with.
func redactQuoted(value string) string {
	bare := strings.Trim(value, `"'`)
	if bare == "" {
		return value
	}
	start := strings.Index(value, bare)
	return value[:start] + redactedValue + value[start+len(bare):]
}

// isSecretName reports whether an assignment's or a flag's name is a secret's: --api-key is checked as API_KEY.
func isSecretName(name string) bool {
	name = strings.ReplaceAll(strings.TrimLeft(name, "-"), "-", "_")
	return name != "" && envutil.IsSecretKey(name, nil)
}
