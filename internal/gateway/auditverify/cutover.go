package auditverify

import (
	"database/sql"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Where hash v2 starts decides which events have their HMAC checked: hash v2 events do, legacy (v1) events can't.
// The database's own answer (its first event with hash_version 2) proves nothing: someone who can write to the
// database, without the secret, can relabel hash v2 events as legacy and then edit them. The start has to be
// confirmed outside the database, by a pinned value (--hash-v2-from or AUDIT_HASH_V2_FROM) or by a format 2
// anchor, which records it.

// hashV2Start is the start the chain walk checks events against: the pinned one if there is one, otherwise the
// database's.
func hashV2Start(head db.AuditChainHead, pinned *int64) sql.NullInt64 {
	if pinned != nil {
		return sql.NullInt64{Int64: *pinned, Valid: true}
	}
	return head.HashV2From
}

// hasLegacyEvents reports whether the database has events from before hash v2: events before its first hash v2
// event, or events but no hash v2 event at all.
func hasLegacyEvents(head db.AuditChainHead) bool {
	return !head.Empty && (!head.HashV2From.Valid || head.HashV2From.Int64 > 0)
}

// checkPinnedHashV2Start compares the pinned start with the database. The start is pinned once the upgraded gateway
// has written hash v2 events, so a database without any fails too: they were relabeled as legacy, or removed from
// the end. The legacy events left would pass, with only their links checked.
func checkPinnedHashV2Start(head db.AuditChainHead, pinned *int64, report *Report) {
	if pinned == nil {
		return
	}
	actual := head.HashV2From
	switch {
	case actual.Valid && actual.Int64 != *pinned:
		report.fail("the database has hash v2 from event %d, but the pinned hash v2 start (--hash-v2-from or "+
			"AUDIT_HASH_V2_FROM) is event %d", actual.Int64, *pinned)
	case !actual.Valid && !head.Empty && head.LastIndex >= *pinned:
		report.fail("the database has no hash v2 events, but the pinned hash v2 start (--hash-v2-from or "+
			"AUDIT_HASH_V2_FROM) is event %d and events run to %d (hash v2 events relabeled as legacy?)",
			*pinned, head.LastIndex)
	case !actual.Valid:
		report.fail("the database has no hash v2 events, but the pinned hash v2 start (--hash-v2-from or "+
			"AUDIT_HASH_V2_FROM) is event %d and %s: hash v2 events were removed from the end (or the upgraded "+
			"gateway hasn't written one yet)", *pinned, describeChainEnd(head))
	}
}

func describeChainEnd(head db.AuditChainHead) string {
	if head.Empty {
		return "the chain has no events"
	}
	return fmt.Sprintf("the chain ends at event %d", head.LastIndex)
}

// checkHashV2StartConfirmed fails when the database has legacy events and nothing outside it confirms where hash
// v2 starts. That includes a database with no hash v2 events at all, which is what relabeling every hash v2 event
// produces.
func checkHashV2StartConfirmed(head db.AuditChainHead, report *Report) {
	if report.HashV2FromPinned != nil || report.HashV2StartAnchors > 0 || !hasLegacyEvents(head) {
		return
	}
	report.fail("nothing outside the database confirms where hash v2 starts (the database %s), so hash v2 "+
		"events relabeled as legacy to skip their HMAC check would go unnoticed. Pin the start you recorded after "+
		"upgrading with --hash-v2-from or AUDIT_HASH_V2_FROM (event %d if the database is intact), or check "+
		"anchors written since the upgrade", describeHashV2Start(head.HashV2From), firstHashV2Event(head))
}

// firstHashV2Event is where hash v2 starts according to the database: its first hash v2 event or, before there is
// one, the next event.
func firstHashV2Event(head db.AuditChainHead) int64 {
	if head.HashV2From.Valid {
		return head.HashV2From.Int64
	}
	return head.LastIndex + 1
}

func describeHashV2Start(start sql.NullInt64) string {
	if !start.Valid {
		return "has no hash v2 events"
	}
	return fmt.Sprintf("has hash v2 from event %d", start.Int64)
}
