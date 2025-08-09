package rbac

import (
	"regexp"
	"strings"
)

// keyMatch3Multi is a custom matcher that extends keyMatch3 behavior:
// - {var} matches single path segment (no slashes)
// - {var:.*} matches multiple path segments (including slashes)
// - * matches anything within a segment
//
// Examples:
//   /apps/{app}/processes/{pid} matches /apps/myapp/processes/p1
//   but NOT /apps/myapp/processes/p1/exec
//
//   /apps/{app}/objects/{key:.*} matches /apps/myapp/objects/path/to/file.txt
func keyMatch3Multi(args ...interface{}) (interface{}, error) {
	if len(args) < 2 {
		return false, nil
	}
	
	path, ok1 := args[0].(string)
	pattern, ok2 := args[1].(string)
	if !ok1 || !ok2 {
		return false, nil
	}

	// First, handle multi-segment patterns BEFORE escaping
	// Replace {var:.*} with a placeholder that will survive QuoteMeta
	multiPlaceholder := "<<<MULTI_SEGMENT_MATCH>>>"
	multiSegmentRe := regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*):\.\*\}`)
	pattern = multiSegmentRe.ReplaceAllString(pattern, multiPlaceholder)

	// Replace {var} with a placeholder
	singlePlaceholder := "<<<SINGLE_SEGMENT_MATCH>>>"
	singleSegmentRe := regexp.MustCompile(`\{[a-zA-Z_][a-zA-Z0-9_]*\}`)
	pattern = singleSegmentRe.ReplaceAllString(pattern, singlePlaceholder)

	// Now escape regex special characters
	escaped := regexp.QuoteMeta(pattern)

	// Replace placeholders with actual regex patterns
	escaped = strings.ReplaceAll(escaped, multiPlaceholder, `(.+)`)
	escaped = strings.ReplaceAll(escaped, singlePlaceholder, `([^/]+)`)

	// Handle literal * wildcards (match within segment)
	escaped = strings.ReplaceAll(escaped, `\*`, `[^/]*`)

	// Create regex with anchors for exact matching
	re, err := regexp.Compile("^" + escaped + "$")
	if err != nil {
		return false, err
	}

	return re.MatchString(path), nil
}