package auditverify_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

const unconfirmedStart = "nothing outside the database confirms where hash v2 starts"

func TestVerifyFailsWhenNothingConfirmsTheHashV2Start(t *testing.T) {
	database := newAuditDatabase(t)
	insertLegacyEvents(t, database)
	writeEvents(t, database, 2) // hash v2 from event 3

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, unconfirmedStart, "has hash v2 from event 3", "event 3 if the database is intact")
	assert.Equal(t, 1, report.FailureCount, "failures: %q", report.Failures)
	var summary bytes.Buffer
	require.NoError(t, report.Write(&summary))
	assert.Contains(t, summary.String(), "hash v2 start: NOT confirmed outside the database")

	requireOK(t, verify(t, database, auditverify.Options{HashV2From: int64Ptr(3)}))

	report = verify(t, database, auditverify.Options{HashV2From: int64Ptr(2)})
	requireFailure(t, report, "the database has hash v2 from event 3, but the pinned hash v2 start", "is event 2")
	requireFailure(t, report, "event 2 ", "marked as a legacy (v1) event after hash v2 started at event 2")
}

func TestVerifyNeedsNoPinWithoutLegacyEvents(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 2)
	requireOK(t, verify(t, database, auditverify.Options{}))
}

func TestVerifyDetectsEveryV2EventRelabeledAsLegacy(t *testing.T) {
	database := newAuditDatabase(t)
	insertLegacyEvents(t, database)
	writeEvents(t, database, 3) // hash v2 from event 3
	bucket := fakes3.New(firstHour)
	writeAnchor(t, database, bucket, firstHour) // records hash v2 starting at event 3
	// Relabeled, the edits skip the HMAC check, and the database looks like no event was written since the upgrade.
	tamper(t, database, `UPDATE audit.audit_event SET hash_version = 1, details = '{"n":9}' WHERE chain_index >= 3`)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, unconfirmedStart, "has no hash v2 events", "event 6 if the database is intact")
	assert.Equal(t, int64(0), report.V2Events)

	report = verify(t, database, auditverify.Options{HashV2From: int64Ptr(3)})
	requireFailure(t, report, "the database has no hash v2 events, but the pinned hash v2 start",
		"is event 3 and events run to 5")
	requireFailure(t, report, "event 4 ", "marked as a legacy (v1) event after hash v2 started at event 3")

	report = verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour))
	requireFailure(t, report, "records hash v2 starting at event 3, but the database has no hash v2 events")
	assert.NotContains(t, strings.Join(report.Failures, "\n"), unconfirmedStart, "the anchor confirms the start")
}

func TestVerifyChecksTheStartAgainstAnAnchorFromBeforeTheFirstV2Event(t *testing.T) {
	database := newAuditDatabase(t)
	insertLegacyEvents(t, database)
	bucket := fakes3.New(firstHour)
	// The upgraded gateway's first anchor, before it wrote any event: every event after it is hash v2.
	writeAnchor(t, database, bucket, firstHour)
	writeEvents(t, database, 3)
	now := firstHour.Add(time.Hour)
	requireOK(t, verifyWithAnchors(t, database, bucket, now))

	tamper(t, database, `UPDATE audit.audit_event SET hash_version = 1, details = '{"n":9}' WHERE chain_index = 3`)
	requireFailure(t, verifyWithAnchors(t, database, bucket, now),
		"records no hash v2 events up to event 2, so hash v2 starts at event 3,",
		"but the database has hash v2 from event 4")

	tamper(t, database, `UPDATE audit.audit_event SET hash_version = 1 WHERE chain_index > 3`)
	requireFailure(t, verifyWithAnchors(t, database, bucket, now),
		"so hash v2 starts at event 3, but the database has no hash v2 events")
}

func TestVerifyBeforeTheFirstV2Event(t *testing.T) {
	database := newAuditDatabase(t)
	insertLegacyEvents(t, database)

	// Right after upgrading, before the first hash v2 event, nothing confirms where hash v2 will start.
	requireFailure(t, verify(t, database, auditverify.Options{}), unconfirmedStart, "event 3 if the database is intact")
	// A pin isn't enough yet: a chain without hash v2 events is also what removing every one of them leaves.
	requireFailure(t, verify(t, database, auditverify.Options{HashV2From: int64Ptr(3)}),
		"the database has no hash v2 events, but the pinned hash v2 start", "is event 3 and the chain ends at event 2")
	// The upgraded gateway's first anchor confirms it.
	bucket := fakes3.New(firstHour)
	writeAnchor(t, database, bucket, firstHour)
	requireOK(t, verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour)))
}

// With the start pinned, removing every hash v2 event from the end leaves legacy events that would pass: it fails.
func TestVerifyDetectsEveryV2EventRemovedWithAPinnedStart(t *testing.T) {
	database := newAuditDatabase(t)
	insertLegacyEvents(t, database)
	writeEvents(t, database, 3) // hash v2 from event 3
	requireOK(t, verify(t, database, auditverify.Options{HashV2From: int64Ptr(3)}))

	tamper(t, database, `DELETE FROM audit.audit_event WHERE chain_index >= 3`)
	requireFailure(t, verify(t, database, auditverify.Options{HashV2From: int64Ptr(3)}),
		"hash v2 events were removed from the end")
}
