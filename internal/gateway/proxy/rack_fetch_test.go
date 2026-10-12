package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// url.JoinPath resolves "." and "..", so a rack read never takes such a segment from a request's path.
func TestEscapePathSegments(t *testing.T) {
	for _, segment := range []string{"", ".", ".."} {
		_, err := escapePathSegments([]string{"apps", segment, "releases"})
		require.Error(t, err, segment)
	}
	escaped, err := escapePathSegments([]string{"apps", "a b", "x/y"})
	require.NoError(t, err)
	require.Equal(t, []string{"apps", "a%20b", "x%2Fy"}, escaped)
}
