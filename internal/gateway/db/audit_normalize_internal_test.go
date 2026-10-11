package db

import (
	"database/sql"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateAuditText(t *testing.T) {
	assert.Equal(t, "short", truncateAuditText("short", 10))

	long := strings.Repeat("é", 9011)
	truncated := truncateAuditText(long, 8000)
	assert.Equal(t, 8000, utf8.RuneCountInString(truncated))
	assert.True(t, strings.HasSuffix(truncated, "…[truncated from 9011 characters]"))

	// Too short for the marker: cut without one.
	assert.Equal(t, strings.Repeat("d", 16), truncateAuditText(strings.Repeat("d", 40), 16))
}

func TestSanitizeAuditText(t *testing.T) {
	assert.Equal(t, "bad\uFFFDbyte\uFFFD", sanitizeAuditText("bad\x00byte\xff"))
	assert.Equal(t, "fine ✓", sanitizeAuditText("fine ✓"))
}

func TestCanonicalAuditIP(t *testing.T) {
	cases := []struct {
		input    string
		expected sql.NullString
	}{
		{" 203.0.113.10 ", sql.NullString{String: "203.0.113.10", Valid: true}},
		{"2001:DB8::0:1", sql.NullString{String: "2001:db8::1", Valid: true}},
		{"fe80::1%eth0", sql.NullString{String: "fe80::1", Valid: true}},
		{"::ffff:10.1.2.3", sql.NullString{String: "::ffff:10.1.2.3", Valid: true}},
		{"not-an-ip", sql.NullString{}},
		{"", sql.NullString{}},
	}
	for _, c := range cases {
		input, expected := c.input, c.expected
		assert.Equal(t, expected, canonicalAuditIP(input), "input %q", input)
	}
}

func TestStoredAuditIP(t *testing.T) {
	cases := map[string]sql.NullString{
		"203.0.113.10/32":     {String: "203.0.113.10", Valid: true},
		"2001:db8::1/128":     {String: "2001:db8::1", Valid: true},
		"::ffff:10.1.2.3/128": {String: "::ffff:10.1.2.3", Valid: true},
		"::1.2.3.4/128":       {String: "::102:304", Valid: true}, // how Postgres prints a v4-compatible address
		"203.0.113.0/24":      {String: "203.0.113.0/24", Valid: true},
		"garbage-from-tamper": {String: "garbage-from-tamper", Valid: true},
	}
	for input, expected := range cases {
		assert.Equal(t, expected, storedAuditIP(sql.NullString{String: input, Valid: true}), "input %q", input)
	}
	assert.Equal(t, sql.NullString{}, storedAuditIP(sql.NullString{}))
}

func TestNewAuditChainRowNormalizesValues(t *testing.T) {
	before := time.Now()
	row := newAuditChainRow(&AuditLog{
		UserEmail:      "alice@example.com",
		APITokenName:   "   ",
		RequestID:      " req-1 ",
		HTTPStatus:     0,
		ResponseTimeMs: -5,
		EventCount:     0,
	})

	assert.Equal(t, AuditHashVersion, row.HashVersion)
	assert.False(t, row.Timestamp.Before(before.Truncate(time.Microsecond)))
	assert.Equal(t, time.UTC, row.Timestamp.Location())
	assert.Zero(t, row.Timestamp.Nanosecond()%1000)
	assert.Equal(t, sql.NullString{}, row.APITokenName)
	assert.Equal(t, sql.NullString{String: "req-1", Valid: true}, row.RequestID)
	assert.Equal(t, sql.NullString{String: "", Valid: true}, row.UserName)
	assert.Equal(t, sql.NullInt32{Int32: 0, Valid: true}, row.HTTPStatus)
	assert.Equal(t, int32(0), row.ResponseTimeMs)
	assert.Equal(t, int32(1), row.EventCount)
	assert.Equal(t, sql.NullString{}, row.IPAddress)
}
