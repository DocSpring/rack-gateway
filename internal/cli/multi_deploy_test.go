package cli

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefixedWriter(t *testing.T) {
	t.Run("prefixes complete lines", func(t *testing.T) {
		var buf bytes.Buffer
		mu := &sync.Mutex{}
		pw := newPrefixedWriter(&buf, "staging", "", mu)

		_, err := pw.Write([]byte("hello world\n"))
		require.NoError(t, err)

		assert.Equal(t, "[staging] hello world\n", buf.String())
	})

	t.Run("buffers partial lines until newline", func(t *testing.T) {
		var buf bytes.Buffer
		mu := &sync.Mutex{}
		pw := newPrefixedWriter(&buf, "us", "", mu)

		// Write partial line
		_, err := pw.Write([]byte("partial"))
		require.NoError(t, err)
		assert.Empty(t, buf.String())

		// Write rest of line
		_, err = pw.Write([]byte(" line\n"))
		require.NoError(t, err)
		assert.Equal(t, "[us] partial line\n", buf.String())
	})

	t.Run("handles multiple lines in one write", func(t *testing.T) {
		var buf bytes.Buffer
		mu := &sync.Mutex{}
		pw := newPrefixedWriter(&buf, "eu", "", mu)

		_, err := pw.Write([]byte("line1\nline2\nline3\n"))
		require.NoError(t, err)

		expected := "[eu] line1\n[eu] line2\n[eu] line3\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("flush outputs remaining buffer", func(t *testing.T) {
		var buf bytes.Buffer
		mu := &sync.Mutex{}
		pw := newPrefixedWriter(&buf, "dev", "", mu)

		_, err := pw.Write([]byte("no newline"))
		require.NoError(t, err)
		assert.Empty(t, buf.String())

		pw.Flush()
		assert.Equal(t, "[dev] no newline\n", buf.String())
	})

	t.Run("flush does nothing when buffer is empty", func(t *testing.T) {
		var buf bytes.Buffer
		mu := &sync.Mutex{}
		pw := newPrefixedWriter(&buf, "empty", "", mu)

		pw.Flush()
		assert.Empty(t, buf.String())
	})
}

func TestBytesIndexByte(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		search   byte
		expected int
	}{
		{"finds first occurrence", []byte("hello\nworld"), '\n', 5},
		{"returns -1 when not found", []byte("hello world"), '\n', -1},
		{"handles empty slice", []byte{}, '\n', -1},
		{"finds at start", []byte("\nhello"), '\n', 0},
		{"finds at end", []byte("hello\n"), '\n', 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesIndexByte(tt.input, tt.search)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveRacks(t *testing.T) {
	t.Run("splits comma-separated racks", func(t *testing.T) {
		racks, err := resolveRacks("staging,us,eu")
		require.NoError(t, err)
		assert.Equal(t, []string{"staging", "us", "eu"}, racks)
	})

	t.Run("trims whitespace", func(t *testing.T) {
		racks, err := resolveRacks("staging , us , eu")
		require.NoError(t, err)
		assert.Equal(t, []string{"staging", "us", "eu"}, racks)
	})

	t.Run("removes empty entries", func(t *testing.T) {
		racks, err := resolveRacks("staging,,us,")
		require.NoError(t, err)
		assert.Equal(t, []string{"staging", "us"}, racks)
	})

	t.Run("returns error for empty input", func(t *testing.T) {
		_, err := resolveRacks("")
		assert.Error(t, err)
	})

	t.Run("returns error for only whitespace", func(t *testing.T) {
		_, err := resolveRacks("  ,  ,  ")
		assert.Error(t, err)
	})
}

func TestMultiDeployCommand(t *testing.T) {
	cmd := MultiDeployCommand()

	t.Run("has correct command name", func(t *testing.T) {
		assert.Equal(t, "multi-deploy [dir]", cmd.Use)
	})

	t.Run("has required racks flag", func(t *testing.T) {
		racksFlag := cmd.Flags().Lookup("racks")
		require.NotNil(t, racksFlag)
	})

	t.Run("has standard deploy flags", func(t *testing.T) {
		assert.NotNil(t, cmd.Flags().Lookup("app"))
		assert.NotNil(t, cmd.Flags().Lookup("manifest"))
		assert.NotNil(t, cmd.Flags().Lookup("wait"))
		assert.NotNil(t, cmd.Flags().Lookup("no-cache"))
		assert.NotNil(t, cmd.Flags().Lookup("description"))
		assert.NotNil(t, cmd.Flags().Lookup("file"))
		assert.NotNil(t, cmd.Flags().Lookup("replace"))
	})
}

func TestMultiDeployColorsEnabled(t *testing.T) {
	// This test verifies the function doesn't panic and returns a boolean
	// Actual color detection depends on environment
	result := multiDeployColorsEnabled()
	// Result should be a boolean - either true or false is valid
	assert.IsType(t, true, result)
}

func TestPrefixedWriterImplementsWriter(t *testing.T) {
	var buf bytes.Buffer
	mu := &sync.Mutex{}
	pw := newPrefixedWriter(&buf, "test", "", mu)

	// Verify it implements io.Writer by writing
	n, err := pw.Write([]byte("test\n"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
}

func TestRackColors(t *testing.T) {
	// Ensure we have enough colors for typical multi-rack setups
	assert.GreaterOrEqual(t, len(rackColors), 6)
}

func TestMultiDeployColorReset(t *testing.T) {
	// Verify reset code is standard ANSI reset
	assert.Equal(t, "\033[0m", multiDeployColorReset)
}
