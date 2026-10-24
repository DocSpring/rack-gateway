package db

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Details over the limit stay valid JSON: the longest strings are shortened first, so short fields such as IDs and
// an approver's notes survive a long message.
func TestFitAuditDetailsKeepsValidJSON(t *testing.T) {
	message := strings.Repeat(`<"é`, 20000)
	details, err := json.Marshal(map[string]interface{}{
		"approval_id": 42,
		"notes":       "approved after review",
		"message":     message,
		"nested":      map[string]interface{}{"list": []interface{}{"a", strings.Repeat("b", 3000)}},
	})
	require.NoError(t, err)

	fitted := fitAuditDetails(string(details))
	require.LessOrEqual(t, utf8.RuneCountInString(fitted), auditDetailsMax)
	var stored map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(fitted), &stored), fitted)
	assert.EqualValues(t, 42, stored["approval_id"])
	assert.Equal(t, "approved after review", stored["notes"])
	cut := stored["message"].(string)
	prefix, ok := strings.CutSuffix(cut, truncationMarker(utf8.RuneCountInString(message)))
	require.True(t, ok, "the message is marked with its length")
	assert.True(t, strings.HasPrefix(message, prefix))
	assert.Equal(t, strings.Repeat("b", 3000), stored["nested"].(map[string]interface{})["list"].([]interface{})[1])
}

// Details within the limit are stored as written, and anything that isn't a JSON object is cut at the end.
func TestFitAuditDetailsLeavesOtherDetailsAlone(t *testing.T) {
	assert.Equal(t, `{"b":"<","a":1}`, fitAuditDetails(`{"b":"<","a":1}`))

	text := strings.Repeat("x", auditDetailsMax+10)
	assert.Equal(t, truncateAuditText(text, auditDetailsMax), fitAuditDetails(text))

	keys := map[string]interface{}{}
	for i := range 2000 {
		keys[strings.Repeat("k", 5)+string(rune('a'+i%26))+strings.Repeat("z", i/26)] = 1
	}
	manyKeys, err := json.Marshal(keys)
	require.NoError(t, err)
	assert.Equal(t, truncateAuditText(string(manyKeys), auditDetailsMax), fitAuditDetails(string(manyKeys)),
		"an object too long without any strings to shorten is cut")
}
