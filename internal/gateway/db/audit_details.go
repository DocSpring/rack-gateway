package db

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// maxDetailsShrinks bounds how many string values fitAuditDetails shortens before it cuts the details instead.
const maxDetailsShrinks = 64

// fitAuditDetails is details as stored: valid UTF-8 without NUL bytes, at most auditDetailsMax characters. A JSON
// object that's too long stays valid JSON: it's re-encoded without HTML escaping, then its longest string values are
// shortened (each marked with its original length) until it fits, so short fields such as IDs and notes survive one
// long value such as a request's message. Anything else is cut at the end (truncateAuditText).
func fitAuditDetails(details string) string {
	details = sanitizeAuditText(details)
	if utf8.RuneCountInString(details) <= auditDetailsMax {
		return details
	}
	if fitted, ok := shrinkJSONStrings(details, auditDetailsMax); ok {
		return fitted
	}
	return truncateAuditText(details, auditDetailsMax)
}

// shrinkJSONStrings fits a JSON object in limit characters by shortening its longest strings, and reports whether
// it could.
func shrinkJSONStrings(details string, limit int) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(details))
	decoder.UseNumber()
	var object map[string]interface{}
	if err := decoder.Decode(&object); err != nil || object == nil {
		return "", false
	}
	for range maxDetailsShrinks {
		encoded, err := encodeDetails(object)
		if err != nil {
			return "", false
		}
		excess := utf8.RuneCountInString(encoded) - limit
		if excess <= 0 {
			return encoded, true
		}
		longest, replace := longestJSONString(object)
		length := utf8.RuneCountInString(longest)
		marker := truncationMarker(length)
		keep := length - excess - utf8.RuneCountInString(marker)
		if keep < 0 {
			if length <= utf8.RuneCountInString(marker) {
				return "", false
			}
			keep = 0
		}
		replace(string([]rune(longest)[:keep]) + marker)
	}
	return "", false
}

// encodeDetails encodes value as compact JSON, without escaping <, > and & (each would take six characters).
func encodeDetails(value interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// longestJSONString returns the longest string value in value (objects and arrays are walked, in key order) and a
// function that replaces it. Ties go to the first found.
func longestJSONString(value interface{}) (string, func(string)) {
	finder := &longestStringFinder{length: -1, replace: func(string) {}}
	finder.walk(value)
	return finder.longest, finder.replace
}

// longestStringFinder is the longest string found so far in a walk of a decoded JSON value, and how to replace it.
type longestStringFinder struct {
	longest string
	length  int
	replace func(string)
}

func (f *longestStringFinder) consider(item interface{}, set func(string)) {
	if text, ok := item.(string); ok {
		if length := utf8.RuneCountInString(text); length > f.length {
			f.longest, f.length, f.replace = text, length, set
		}
	}
}

func (f *longestStringFinder) walk(node interface{}) {
	switch typed := node.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			f.consider(typed[key], func(text string) { typed[key] = text })
			f.walk(typed[key])
		}
	case []interface{}:
		for i := range typed {
			f.consider(typed[i], func(text string) { typed[i] = text })
			f.walk(typed[i])
		}
	}
}
