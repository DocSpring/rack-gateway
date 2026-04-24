package dockerignore

import (
	"io"

	"github.com/moby/patternmatcher/ignorefile"
)

// ReadAll keeps Convox's deprecated BuildKit import on the current Moby parser.
func ReadAll(reader io.Reader) ([]string, error) {
	return ignorefile.ReadAll(reader)
}
