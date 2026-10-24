package audit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The limit is on the command as the audit log records it: redacted, then JSON-encoded.
func TestCheckCommandLengthMeasuresTheRecordedCommand(t *testing.T) {
	// A JSON string adds two quotes.
	atLimit := strings.Repeat("x", MaxRecordedCommandBytes-2)
	require.NoError(t, CheckCommandLength(atLimit))

	err := CheckCommandLength(atLimit + "x")
	require.ErrorContains(t, err, "too long for the audit log to keep in full")
	require.ErrorContains(t, err, "51201 bytes as recorded, at most 51200, or 50 KB")

	// Each < is recorded as \u003c, six bytes.
	require.Error(t, CheckCommandLength(strings.Repeat("<", MaxRecordedCommandBytes/6+1)))
}
