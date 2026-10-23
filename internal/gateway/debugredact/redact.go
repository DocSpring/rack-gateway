// Package debugredact takes credentials and secrets out of what the gateway writes to its debug logs: headers,
// JSON bodies and URLs. Debug topics get turned on to diagnose problems, sometimes in production, so nothing they
// log may hold a session or API token, a cookie, an MFA code, a login code, a password or an env var's value.
package debugredact

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Redacted stands in for a value that isn't logged.
const Redacted = "[REDACTED]"

// sensitiveNameParts mark a header, JSON field or query parameter that carries a credential or secret wherever
// they appear in its name: "Authorization", "Cookie", "X-CSRF-Token", "X-Postmark-Server-Token", "api_token",
// "assertion_response".
var sensitiveNameParts = []string{
	"apikey",
	"assertion",
	"authorization",
	"cookie",
	"credential",
	"csrf",
	"otp",
	"passwd",
	"password",
	"private",
	"secret",
	"signature",
	"token",
}

// sensitiveNameWords mark a credential when they're a whole word of a name ("code", "login_code",
// "backup_codes", "session_data", "X-Api-Key", a presigned URL's "sig"). They're too common inside other words to
// match anywhere.
var sensitiveNameWords = map[string]struct{}{
	"auth":    {},
	"code":    {},
	"codes":   {},
	"key":     {},
	"pin":     {},
	"session": {},
	"sig":     {},
	"state":   {},
}

// sensitiveNames are sensitive only as the whole name. A rack resource's "url" holds its credentials
// (postgres://user:password@host/db), but "build_url" or "pr_url" don't. The command an exec or run sends (the
// Command header) can carry a credential on its command line.
var sensitiveNames = map[string]struct{}{
	"command": {},
	"url":     {},
}

// urlHeaders hold a URL as their whole value, which may be relative (a redirect's Location is often just a path
// and query).
var urlHeaders = map[string]struct{}{
	"Content-Location": {},
	"Location":         {},
	"Referer":          {},
}

// absoluteURLPattern finds absolute URLs inside a longer value, such as a Link header or a JSON string.
var absoluteURLPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s<>"']+`)

// IsSensitiveName reports whether a header, JSON field or query parameter named name carries a credential or
// secret.
func IsSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := sensitiveNames[lower]; ok {
		return true
	}
	for _, part := range sensitiveNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, word := range words {
		if _, ok := sensitiveNameWords[word]; ok {
			return true
		}
	}
	return false
}

// isSensitiveHeader reports whether a header's value must not be logged at all. Every MFA header (X-MFA-TOTP,
// X-MFA-WebAuthn, ...) is, since some carry a code or an assertion.
func isSensitiveHeader(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "x-mfa-") || IsSensitiveName(name)
}

// HeaderValue is a header's value as it may be logged. A sensitive header's value is redacted. A URL in any other
// header loses its credentials and sensitive query values (see URL): a redirect's Location can carry a login code
// and state, such as the one that hands a login back to the CLI.
func HeaderValue(name, value string) string {
	if isSensitiveHeader(name) {
		return Redacted
	}
	if _, ok := urlHeaders[http.CanonicalHeaderKey(name)]; ok {
		return URL(value)
	}
	return absoluteURLPattern.ReplaceAllStringFunc(value, URL)
}

// Headers lists headers one per line, sorted, with their values as they may be logged (see HeaderValue).
func Headers(headers http.Header) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		for _, value := range headers[key] {
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(HeaderValue(key, value))
			builder.WriteByte('\n')
		}
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

// URL is a URL as it may be logged: without its user info (which can hold a password) or fragment (which can hold
// an OAuth token), and with the values of sensitive query parameters (a login code, an OAuth state, a token)
// redacted. A URL that can't be parsed is redacted whole.
func URL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return Redacted
	}
	parsed.User = nil
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.RawQuery != "" {
		parsed.RawQuery = RedactQuery(parsed.RawQuery)
	}
	return parsed.String()
}

// RedactQuery redacts the values of sensitive parameters in a query string (an api_key, a client_secret, a login
// code; see IsSensitiveName), or all of it when it can't be parsed. The audit log and the request logs keep a
// request's query this way too.
func RedactQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return url.QueryEscape(Redacted)
	}
	for key := range values {
		if IsSensitiveName(key) {
			values[key] = []string{Redacted}
		}
	}
	return values.Encode()
}

// HasSensitiveQuery reports whether a query string gives a value to a sensitive parameter (an api_key, a token, a
// signature; see IsSensitiveName). One that can't be parsed counts as sensitive, as RedactQuery redacts it whole.
func HasSensitiveQuery(rawQuery string) bool {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return true
	}
	for key, vals := range values {
		if !IsSensitiveName(key) {
			continue
		}
		for _, value := range vals {
			if value != "" {
				return true
			}
		}
	}
	return false
}
