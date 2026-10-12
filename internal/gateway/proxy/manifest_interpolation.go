package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The rack substitutes ${...} in a manifest's raw text before it parses the YAML: Convox's manifest.Load replaces
// every match of \$\{([^}]*?)\} with a value from the app's environment (and, while building, the build's build
// arguments), or with "" when the name isn't set. A match can span lines, quotes and comments, and an unset name
// simply vanishes, so a manifest can parse one way as the gateway reads it and another as the rack builds it:
// "${X}image: attacker/app" becomes an image key, and ${X}{"image":"attacker/app"} a mapping.
//
// So the gateway accepts ${...} only as a ${NAME} reference placed where no value can change anything but that one
// value: on a "key: value" or "- value" line, inside a quoted value, or in an unquoted value made only of
// characters that can't form YAML structure. An unset name leaves a shorter scalar; a value without line breaks or
// quotes (the gateway refuses those in build arguments, and env is set by people allowed to set it) stays inside
// it. Image values can't hold a reference at all (see imagePolicy), so substitution never changes an image.

var (
	interpolationRef = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

	// interpolatedLine is a line that may hold references: indentation, any sequence dashes and an optional plain
	// key (Kubernetes annotation and label keys have a "/"), then the value (group 1) and an optional comment
	// (group 2).
	interpolatedLine = regexp.MustCompile(`^ *(?:- +)*(?:[A-Za-z0-9_./-]+: +)?("[^"\\]*"|'[^']*'|[^ "'#]+)( +#.*| *)$`)

	// interpolatedPlainValue is an unquoted value of references and characters that can't form YAML structure:
	// no spaces, colons, quotes, brackets, braces, anchors, aliases, tags, block indicators or comments.
	interpolatedPlainValue = regexp.MustCompile(`^(?:[A-Za-z0-9_./@+=~,%-]|\$\{[A-Za-z_][A-Za-z0-9_]*\})+$`)

	// plainValueStart is how an unquoted value must start once its references are gone (as unset names are): not
	// with an indicator such as "-", which could make it a sequence entry.
	plainValueStart = regexp.MustCompile(`^[A-Za-z0-9_/]`)
)

// checkInterpolation refuses a manifest whose ${...} could change its structure when the rack substitutes it.
func checkInterpolation(manifest []byte) error {
	if !bytes.Contains(manifest, []byte("${")) {
		return nil
	}
	for i, line := range strings.Split(string(manifest), "\n") {
		if !strings.Contains(line, "${") {
			continue
		}
		if err := checkInterpolatedLine(strings.TrimSuffix(line, "\r")); err != nil {
			return fmt.Errorf(
				"manifest line %d %w, refused because the rack substitutes ${...} before it parses the manifest",
				i+1, err,
			)
		}
	}
	return nil
}

func checkInterpolatedLine(line string) error {
	// Every "${" must start a ${NAME} reference: anything else, the rack's pattern would carry on to the next "}",
	// across lines, quotes and comments.
	if strings.Contains(interpolationRef.ReplaceAllString(line, ""), "${") {
		return errors.New("uses ${...} other than ${NAME}")
	}

	match := interpolatedLine.FindStringSubmatch(line)
	if match == nil || strings.Contains(match[2], "${") {
		return errors.New("uses ${NAME} outside a value")
	}
	value := match[1]
	if value[0] == '"' || value[0] == '\'' {
		return nil
	}
	if rest := interpolationRef.ReplaceAllString(value, ""); !interpolatedPlainValue.MatchString(value) ||
		(rest != "" && !plainValueStart.MatchString(rest)) {
		return errors.New("uses ${NAME} in an unquoted value with spaces, colons or YAML indicators (quote the value)")
	}
	return nil
}
