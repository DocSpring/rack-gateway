package auditverify_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
)

// forgedEvent inserts a copy of event 3 with another chain index, and another user.
func forgedEvent(chainIndex string) string {
	return fmt.Sprintf(`INSERT INTO audit.audit_event
		(chain_index, hash_version, "timestamp", previous_hash, event_hash, user_email, action_type, action, status)
		SELECT %s, hash_version, "timestamp", previous_hash, event_hash, 'forged@example.com', action_type, action,
			status
		FROM audit.audit_event WHERE chain_index = 3`, chainIndex)
}

func TestVerifyCountsEventsTheChainWalkCannotSee(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 4)
	// With the constraints that forbid them dropped, forged events can sit where a walk by chain index never
	// looks: outside the chain, or at an index the walk has already passed.
	tamper(t, database,
		`ALTER TABLE audit.audit_event DROP CONSTRAINT chain_index_unique`,
		`ALTER TABLE audit.audit_event DROP CONSTRAINT chain_index_nonnegative`,
		`ALTER TABLE audit.audit_event DROP CONSTRAINT genesis_hash_rules`,
		`ALTER TABLE audit.audit_event ALTER COLUMN chain_index DROP NOT NULL`,
		forgedEvent("NULL"),
		forgedEvent("-1"),
		forgedEvent("1"),
	)

	// With two events per batch, the walk reads one of the two events at index 1, then continues after it.
	report := verify(t, database, auditverify.Options{BatchSize: 2})
	assert.False(t, report.Head.Empty, "an event with a NULL chain index doesn't hide the chain")
	assert.Equal(t, int64(3), report.Head.LastIndex)
	requireFailure(t, report, "events outside the chain (NULL or negative chain_index): 2")
	requireFailure(t, report, "events at a chain index another event already has: 1")
	requireFailure(t, report, "the database has 5 events with chain indexes 0..3, but the chain walk checked 4")

	// A range only checks the chain.
	report = verify(t, database, auditverify.Options{From: 2})
	assert.NotContains(t, strings.Join(report.Failures, "\n"), "outside the chain")
}

func TestVerifyCountsEventsOutsideAnEmptyChain(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 4)
	tamper(t, database,
		`ALTER TABLE audit.audit_event DROP CONSTRAINT chain_index_nonnegative`,
		`ALTER TABLE audit.audit_event DROP CONSTRAINT genesis_hash_rules`,
		forgedEvent("-5"),
		`DELETE FROM audit.audit_event WHERE chain_index >= 0`,
	)

	report := verify(t, database, auditverify.Options{})
	assert.True(t, report.Head.Empty, "an event with a negative chain index isn't the head")
	requireFailure(t, report, "events outside the chain (NULL or negative chain_index): 1")
}

// Events removed from the end leave a valid, shorter chain, and events written after them take their indexes. The
// event an earlier verification checked shows both, even when no event in the range is left to check.
func TestVerifyFailsWhenTheVerifiedEventIsGone(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 5)
	head := verify(t, database, auditverify.Options{}).Head
	verified := &auditverify.VerifiedEvent{Index: head.LastIndex, Hash: head.LastHash}
	requireOK(t, verify(t, database, auditverify.Options{VerifiedThrough: verified}))
	requireOK(t, verify(t, database, auditverify.Options{From: 5, VerifiedThrough: verified}))

	tamper(t, database, `DELETE FROM audit.audit_event WHERE chain_index >= 3`)
	requireOK(t, verify(t, database, auditverify.Options{}))
	report := verify(t, database, auditverify.Options{From: 5, VerifiedThrough: verified})
	requireFailure(t, report, "the chain ends at event 2, but an earlier verification checked it through event 4")

	writeEvents(t, database, 2)
	requireOK(t, verify(t, database, auditverify.Options{}))
	report = verify(t, database, auditverify.Options{From: 5, VerifiedThrough: verified})
	requireFailure(t, report, "event 4 isn't the event an earlier verification checked")

	tamper(t, database, `DELETE FROM audit.audit_event`)
	report = verify(t, database, auditverify.Options{VerifiedThrough: verified})
	requireFailure(t, report, "the chain has no events, but an earlier verification checked it through event 4")
}
