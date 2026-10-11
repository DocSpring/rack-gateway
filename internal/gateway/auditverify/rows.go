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
	walker := &chainWalker{secret: opts.Secret, cutover: head.HashV2From, report: report}
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
	// cutover is the first hash v2 event in the database. Every event before it must be legacy and every event
	// from it on must be v2.
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
// before the first v2 event.
func (w *chainWalker) checkVersion(row *db.AuditChainRow) {
	if row.HashVersion != db.AuditHashVersion && row.HashVersion != db.AuditHashVersionLegacy {
		w.report.fail("event %d (id %d): unknown hash_version %d", row.ChainIndex, row.ID, row.HashVersion)
		return
	}
	if row.HashVersion == db.AuditHashVersionLegacy && w.cutover.Valid && row.ChainIndex > w.cutover.Int64 {
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
