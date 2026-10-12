package auditverify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"database/sql"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// verifyRows walks events From..To in chain order, starting from the event before From.
func verifyRows(ctx context.Context, source ChainSource, opts Options, head db.AuditChainHead, report *Report) error {
	if head.Empty {
		return nil
	}
	to := head.LastIndex
	if opts.To != nil {
		to = min(to, *opts.To)
	}
	walker := &chainWalker{secret: opts.Secret, cutover: hashV2Start(head, opts.HashV2From), report: report}
	if opts.From > 0 {
		prev, err := source.AuditChainRowBefore(ctx, opts.From)
		if err != nil {
			return err
		}
		walker.prev = prev
	}

	after := opts.From - 1
	for after < to {
		rows, err := source.AuditChainRows(ctx, after, to, opts.BatchSize)
		if err != nil {
			return err
		}
		for _, row := range rows {
			walker.check(row)
		}
		if len(rows) < opts.BatchSize {
			return nil
		}
		after = rows[len(rows)-1].ChainIndex
	}
	return nil
}

// chainWalker checks each event against the one before it.
type chainWalker struct {
	secret []byte
	// cutover is where hash v2 starts: the pinned start, or the database's first hash v2 event. Every event from
	// it on must be v2.
	cutover sql.NullInt64
	prev    *db.AuditChainRow
	report  *Report
}

func (w *chainWalker) check(row *db.AuditChainRow) {
	w.report.countEvent(row)
	w.checkVersion(row)
	w.checkLink(row)
	if row.HashVersion == db.AuditHashVersion {
		w.checkHMAC(row)
	}
	w.prev = row
}

// checkVersion catches a v2 event relabeled as legacy to skip its HMAC check: legacy events can only come
// before hash v2 starts.
func (w *chainWalker) checkVersion(row *db.AuditChainRow) {
	if row.HashVersion != db.AuditHashVersion && row.HashVersion != db.AuditHashVersionLegacy {
		w.report.fail("event %d (id %d): unknown hash_version %d", row.ChainIndex, row.ID, row.HashVersion)
		return
	}
	if row.HashVersion == db.AuditHashVersionLegacy && w.cutover.Valid && row.ChainIndex >= w.cutover.Int64 {
		w.report.fail("event %d (id %d): marked as a legacy (v1) event after hash v2 started at event %d",
			row.ChainIndex, row.ID, w.cutover.Int64)
	}
}

// checkLink checks the event's position: the genesis rules for the first event, otherwise the next index
// (legacy events may skip indexes) and a previous_hash equal to the previous event's hash.
func (w *chainWalker) checkLink(row *db.AuditChainRow) {
	if w.prev == nil {
		if row.ChainIndex != 0 {
			w.report.fail("event %d (id %d): the chain doesn't start at the genesis event (index 0)",
				row.ChainIndex, row.ID)
		} else if row.PreviousHash != nil {
			w.report.fail("event 0 (id %d): the genesis event has a previous_hash", row.ID)
		}
		return
	}

	if expected := w.prev.ChainIndex + 1; row.ChainIndex != expected {
		if row.HashVersion == db.AuditHashVersionLegacy {
			w.report.LegacyIndexGaps++
		} else {
			w.report.fail("event %d (id %d): follows event %d; events %d..%d are missing",
				row.ChainIndex, row.ID, w.prev.ChainIndex, expected, row.ChainIndex-1)
		}
	}
	if !bytes.Equal(row.PreviousHash, w.prev.EventHash) {
		w.report.fail("event %d (id %d): previous_hash doesn't match the hash of event %d",
			row.ChainIndex, row.ID, w.prev.ChainIndex)
	}
}

func (w *chainWalker) checkHMAC(row *db.AuditChainRow) {
	expected, err := db.ComputeAuditEventHash(w.secret, row)
	if err != nil {
		w.report.fail("event %d (id %d): can't compute its hash: %v", row.ChainIndex, row.ID, err)
		return
	}
	if !hmac.Equal(expected, row.EventHash) {
		w.report.fail("event %d (id %d): event_hash doesn't match the stored event (it was changed, or "+
			"AUDIT_HMAC_SECRET is wrong)", row.ChainIndex, row.ID)
	}
}

// checkRowCounts compares the number of stored events with the number the walk checked. The walk reads events by
// chain index, so it can't see events with a NULL or negative index, or a second event at an index it has passed.
// The schema forbids all three, unless its constraints were dropped to hide rows. Events written after the run
// started are left out.
func checkRowCounts(ctx context.Context, source ChainSource, head db.AuditChainHead, report *Report) error {
	last := head.LastIndex
	if head.Empty {
		last = -1
	}
	stats, err := source.AuditChainStats(ctx, last)
	if err != nil {
		return err
	}
	if stats.OutsideChain > 0 {
		report.fail("events outside the chain (NULL or negative chain_index): %d", stats.OutsideChain)
	}
	if duplicates := stats.Rows - stats.DistinctIndexes; duplicates > 0 {
		report.fail("events at a chain index another event already has: %d", duplicates)
	}
	if stats.Rows != report.EventsChecked {
		report.fail("the database has %d events with chain indexes 0..%d, but the chain walk checked %d",
			stats.Rows, last, report.EventsChecked)
	}
	return nil
}

// checkVerifiedEvent fails when the chain no longer holds the latest event an earlier verification checked, as it
// was then: events were removed from the end, and perhaps replaced by new ones. Anchors only catch that for events
// an anchor recorded.
func checkVerifiedEvent(
	ctx context.Context,
	source ChainSource,
	head db.AuditChainHead,
	verified *VerifiedEvent,
	report *Report,
) error {
	switch {
	case verified == nil:
		return nil
	case head.Empty:
		report.fail("the chain has no events, but an earlier verification checked it through event %d: events "+
			"were removed", verified.Index)
		return nil
	case head.LastIndex < verified.Index:
		report.fail("the chain ends at event %d, but an earlier verification checked it through event %d: events "+
			"were removed from the end", head.LastIndex, verified.Index)
		return nil
	}
	row, err := source.AuditChainRowAt(ctx, verified.Index)
	if err != nil {
		return err
	}
	if row == nil || !bytes.Equal(row.EventHash, verified.Hash) {
		report.fail("event %d isn't the event an earlier verification checked: events were removed from the end "+
			"and others written in their place, or the event was changed", verified.Index)
	}
	return nil
}
