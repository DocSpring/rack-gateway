package audit

import (
	"net/url"
	"strings"
)

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
			values[key] = []string{"[REDACTED]"}
		}
	}
	return values.Encode()
}
