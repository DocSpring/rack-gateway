package auditverify

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The failure digest identifies every failure, past the keys a report keeps, whatever order they were found in.
func TestReportFailureDigest(t *testing.T) {
	report := func(keys ...string) *Report {
		r := &Report{}
		for _, key := range keys {
			r.fail("%s", key)
		}
		return r
	}
	assert.Equal(t, report("a", "b").FailureDigest(), report("b", "a").FailureDigest())
	assert.NotEqual(t, report("a", "b").FailureDigest(), report("a", "c").FailureDigest())
	assert.NotEqual(t, report("a").FailureDigest(), report("a", "a").FailureDigest(), "a failure found twice counts")

	many := make([]string, maxFailureKeys+1)
	for i := range many {
		many[i] = fmt.Sprintf("event %d", i)
	}
	full := report(many...)
	assert.False(t, full.AllFailureKeys())
	assert.True(t, report(many[:maxFailureKeys]...).AllFailureKeys())

	replaced := append([]string{}, many[:maxFailureKeys]...)
	replaced = append(replaced, "another event")
	other := report(replaced...)
	assert.Equal(t, full.FailureKeys(), other.FailureKeys(), "the kept keys are the same")
	assert.NotEqual(t, full.FailureDigest(), other.FailureDigest())
}
