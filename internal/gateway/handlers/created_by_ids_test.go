package handlers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCreatedByIDsDedupesAndCapsTheList(t *testing.T) {
	ids, ok := parseCreatedByIDs(" R1, R2,,R1 ,R3")
	require.True(t, ok)
	require.Equal(t, []string{"R1", "R2", "R3"}, ids)

	_, ok = parseCreatedByIDs(uniqueIDs(maxCreatedByIDs))
	require.True(t, ok)
	_, ok = parseCreatedByIDs(uniqueIDs(maxCreatedByIDs + 1))
	require.False(t, ok, "more IDs than the cap")
	_, ok = parseCreatedByIDs("R1," + strings.Repeat("x", maxCreatedByIDLength+1))
	require.False(t, ok, "an ID longer than the cap")
	_, ok = parseCreatedByIDs(strings.Repeat(",", maxCreatedByIDs*(maxCreatedByIDLength+1)+1))
	require.False(t, ok, "a parameter longer than any list the cap allows")
}

func uniqueIDs(n int) string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("RABCDEFG%04d", i)
	}
	return strings.Join(ids, ",")
}
