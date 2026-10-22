package audit

import (
	"regexp"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// redactedValue replaces a credential the audit log mustn't keep.
const redactedValue = "[REDACTED]"

// commandURLUserinfo finds a URL's user information in a command (the user:password@ of
// postgres://user:password@host).
var commandURLUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@'"]+@`)

// commandWord finds the words of a command as a shell splits them: a quoted part, spaces and all, belongs to the
// word it's in, and so does a character escaped with a backslash (Authorization:\ Bearer\ ...). An unmatched quote
// or a trailing backslash is a word of its own.
var commandWord = regexp.MustCompile(`(?s)(?:"(?:[^"\\]|\\.)*"|'[^']*'|\\.|[^\s"'\\])+|["'\\]`)

// commandHeader is a header as a command passes one (curl -H 'X-API-Key: value', or X-API-Key:value): its name,
// the colon and any space after it, and its value.
var commandHeader = regexp.MustCompile(`(?s)^([A-Za-z][A-Za-z0-9_-]*)(:\s*)(\S.*)$`)

// credentialHeaders carry a credential, whatever their value looks like.
var credentialHeaders = map[string]bool{"authorization": true, "proxy-authorization": true, "cookie": true}

// RedactCommand returns an exec command as the audit log keeps it, with the credentials a command line commonly
// carries replaced by [REDACTED]: a URL's user information (postgres://user:password@host), the value of a
// secret-named assignment or flag (DB_PASSWORD=..., --api-key=..., --token ...; see envutil.IsSecretKey), a
// credential header's value (see redactHeader), the word after Bearer or Basic, and the credentials common programs
// take with short flags (mysql -psecret, redis-cli -a secret, curl -u user:secret; see credentialFlags). The rest is
// kept, so the record still says what was run. It can't recognize every credential: they belong in the app's env,
// not on a command line.
func RedactCommand(command string) string {
	command = commandURLUserinfo.ReplaceAllString(command, "${1}"+redactedValue+"@")
	var out strings.Builder
	last, program := 0, ""
	var next func(string) string // redacts the next word, when it's a credential
	for _, span := range commandWord.FindAllStringIndex(command, -1) {
		out.WriteString(command[last:span[0]])
		word := command[span[0]:span[1]]
		last = span[1]
		if next != nil {
			out.WriteString(next(word))
			next = nil
			continue
		}
		if name, ok := programName(word); ok {
			program = name
		}
		var redacted string
		redacted, next = redactCommandWord(program, word)
		out.WriteString(redacted)
	}
	out.WriteString(command[last:])
	return out.String()
}

// redactCommandWord redacts the value of a credential header, a secret-named assignment or flag (NAME=value), or a
// credential flag of the program the command runs, and returns how to redact the next word when that's a credential:
// one after a secret-named flag, a program's credential flag, or Bearer or Basic. A quoted word with spaces in it
// (sh -c '...', "host=db password=...") is redacted as a command of its own.
func redactCommandWord(program, word string) (string, func(string) string) {
	if redacted, next, ok := redactProgramFlag(program, word); ok {
		return redacted, next
	}
	bare := strings.Trim(word, `"'`)
	if header, ok := redactHeader(bare); ok {
		return strings.Replace(word, bare, header, 1), nil
	}
	if inner, ok := quotedPhrase(word); ok {
		return word[:1] + RedactCommand(inner) + word[len(word)-1:], nil
	}
	if name, _, ok := strings.Cut(bare, "="); ok {
		if !isSecretName(name) {
			return word, nil
		}
		assign := strings.Index(word, "=")
		return word[:assign+1] + redactQuoted(word[assign+1:]), nil
	}
	secretFlag := strings.HasPrefix(bare, "-") && isSecretName(bare)
	if secretFlag || strings.EqualFold(bare, "bearer") || strings.EqualFold(bare, "basic") {
		return word, redactQuoted
	}
	return word, nil
}

// redactHeader redacts a credential header's whole value in text, a word without its quotes, and reports whether it
// did. A credential header is Authorization, Proxy-Authorization or Cookie, or one with a secret's name (X-API-Key,
// X-Auth-Token; see isSecretName). Without a space after its colon, text is only taken for a header when its name
// has a hyphen or an underscore, so a task such as credentials:edit isn't.
func redactHeader(text string) (string, bool) {
	match := commandHeader.FindStringSubmatch(text)
	if match == nil {
		return text, false
	}
	name, separator := match[1], match[2]
	if !credentialHeaders[strings.ToLower(name)] {
		headerLike := len(separator) > 1 || strings.ContainsAny(name, "-_")
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
