package routematch

import "strings"

var placeholderValues = map[string]string{
	"app":      "my-app",
	"build":    "B123",
	"id":       "ID123",
	"name":     "resource",
	"pid":      "process-123",
	"service":  "web",
	"registry": "docker.io",
	"release":  "REL123",
	"resource": "db",
	"instance": "i-1234567890",
}

func placeholderValue(name string) string {
	if v, ok := placeholderValues[name]; ok {
		return v
	}
	return name
}

// ExamplePath returns a concrete example path matching the RouteSpec pattern.
func ExamplePath(spec RouteSpec) string {
	var b strings.Builder
	pattern := spec.Pattern
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '{':
			j := i + 1
			for j < len(pattern) && pattern[j] != '}' {
				j++
			}
			name := pattern[i+1 : j]
			b.WriteString(placeholderValue(name))
			i = j
		case '*':
			b.WriteString("extra")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Specs returns a copy of the known route specs. Tests must treat the result as read-only.
func Specs() []RouteSpec {
	out := make([]RouteSpec, len(specs))
	copy(out, specs)
	return out
}
