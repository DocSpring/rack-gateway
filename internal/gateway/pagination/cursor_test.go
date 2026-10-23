package pagination

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorRoundTrip(t *testing.T) {
	for _, id := range []int64{1, 12345, 9223372036854775807} {
		cursor, err := ParseCursor((&Cursor{ID: id}).Encode())
		require.NoError(t, err)
		require.Equal(t, id, cursor.ID)
	}

	bare, err := ParseCursor("12345")
	require.NoError(t, err)
	require.Equal(t, int64(12345), bare.ID)

	empty, err := ParseCursor("")
	require.NoError(t, err)
	require.Nil(t, empty)

	for _, invalid := range []string{"not a cursor", "MjAyNS0xMS0wMVQxMjowMDowMFpfMTIzNDU=", "-"} {
		_, err := ParseCursor(invalid)
		require.Errorf(t, err, "cursor %q", invalid)
	}
}
