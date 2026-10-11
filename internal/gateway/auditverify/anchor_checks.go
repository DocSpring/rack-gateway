package auditverify

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
)

// maxListedMissingHours is how many missing anchor hours the warning names.
const maxListedMissingHours = 5

// verifyAnchors checks every anchor in write order against the one before it and against the database, fails
// when there are events but no anchors or the latest anchor is too old (anchoring has stopped), and warns about
// hours without an anchor and events written after the latest anchor.
func verifyAnchors(
	ctx context.Context,
	source ChainSource,
	opts AnchorOptions,
	head db.AuditChainHead,
	now time.Time,
	report *Report,
) error {
	records, err := loadAnchors(ctx, opts, report)
	if err != nil {
		return err
	}
	report.AnchorsChecked = len(records)
	walker := &anchorWalker{
		source:       source,
		chainID:      opts.ChainID,
		head:         head,
		report:       report,
		maxSeq:       -1,
		verifiedSeq:  -1,
		anchoredHour: map[time.Time]bool{},
	}
	for _, record := range records {
		if err := walker.check(ctx, record); err != nil {
			return err
		}
	}
	if len(records) == 0 {
		reportNoAnchors(opts, head, report)
	} else {
		checkLatestAnchorAge(records[len(records)-1], opts.MaxAge, now, report)
	}
	reportMissingHours(walker.anchoredHour, now, report)
	reportUnanchoredTail(head, walker.maxSeq, report)
	return nil
}

// anchorWalker checks each anchor against the anchor written before it.
type anchorWalker struct {
	source  ChainSource
	chainID string
	head    db.AuditChainHead
	report  *Report
	prev    *anchorRecord
	// maxSeq is the highest chain index an anchor has recorded so far (-1 for none).
	maxSeq int64
	// verifiedSeq and verifiedHash are the last anchored event found in the database, so anchors written while
	// no events were added don't each look it up again.
	verifiedSeq  int64
	verifiedHash string
	anchoredHour map[time.Time]bool
}

func (w *anchorWalker) check(ctx context.Context, record *anchorRecord) error {
	defer func() { w.prev = record }()
	if record.invalid {
		return nil
	}
	w.checkLinkToPrevious(record)
	w.checkIdentity(record)
	w.checkSequence(record)
	w.checkHashV2Start(record)
	return w.checkAnchoredEvent(ctx, record)
}

// checkLinkToPrevious checks prev_anchor_hash names the anchor written just before.
func (w *anchorWalker) checkLinkToPrevious(record *anchorRecord) {
	expected := ""
	if w.prev != nil {
		sum := sha256.Sum256(w.prev.raw)
		expected = hex.EncodeToString(sum[:])
	}
	if strings.EqualFold(record.payload.PrevAnchorHash, expected) {
		return
	}
	switch {
	case w.prev == nil:
		w.report.warn("the first anchor (%s) refers to an earlier anchor that is no longer in the bucket "+
			"(expired after its retention period?)", record.key)
	case record.payload.Format >= jobaudit.AnchorFormat:
		w.report.fail("anchor %s: prev_anchor_hash doesn't match the anchor written before it (%s)",
			record.key, w.prev.key)
	default:
		w.report.LegacyAnchorLinkMismatches++
	}
}

// checkIdentity checks the anchor belongs to this chain and sits under the key for its own timestamp.
func (w *anchorWalker) checkIdentity(record *anchorRecord) {
	if record.payload.ChainID != w.chainID {
		w.report.fail("anchor %s: chain_id is %q, expected %q", record.key, record.payload.ChainID, w.chainID)
	}
	ts, err := time.Parse(time.RFC3339, record.payload.TimestampUTC)
	if err != nil {
		w.report.fail("anchor %s: invalid ts_utc %q", record.key, record.payload.TimestampUTC)
		return
	}
	if expected := jobaudit.AnchorKey(w.chainID, ts); expected != record.key {
		w.report.fail("anchor %s: its ts_utc (%s) belongs under %s", record.key, record.payload.TimestampUTC, expected)
	}
	w.anchoredHour[ts.UTC().Truncate(time.Hour)] = true
}

// checkSequence checks the chain only grows: an anchor never records fewer events than an earlier one.
func (w *anchorWalker) checkSequence(record *anchorRecord) {
	payload := record.payload
	if payload.LastHash == "" {
		if w.maxSeq >= 0 {
			w.report.fail("anchor %s records an empty chain, but earlier anchors recorded events up to %d",
				record.key, w.maxSeq)
		}
		return
	}
	if payload.LastSeq < w.maxSeq {
		w.report.fail("anchor %s: last_seq %d is lower than an earlier anchor's %d (events removed from the end?)",
			record.key, payload.LastSeq, w.maxSeq)
	}
	w.maxSeq = max(w.maxSeq, payload.LastSeq)
}

// checkHashV2Start compares where a format 2 anchor puts the start of hash v2 with the database, which catches
// v2 events relabeled as legacy to skip their HMAC check.
func (w *anchorWalker) checkHashV2Start(record *anchorRecord) {
	payload := record.payload
	if payload.Format < jobaudit.AnchorFormat {
		return
	}
	w.report.HashV2StartAnchors++
	expected := anchoredHashV2Start(payload, w.head)
	if actual := w.head.HashV2From; actual != expected {
		w.report.fail("anchor %s: %s, but the database %s",
			record.key, describeAnchoredHashV2Start(payload, expected), describeHashV2Start(actual))
	}
}

// anchoredHashV2Start is where a format 2 anchor puts the start of hash v2, given the database's latest event.
// Format 2 anchors are only written once the hash v2 migration has run, and every event written from then on is
// hash v2. So an anchor without hash_v2_from_seq means hash v2 starts with the first event after the one it
// recorded (if there is one yet).
func anchoredHashV2Start(payload jobaudit.AnchorPayload, head db.AuditChainHead) sql.NullInt64 {
	if payload.HashV2FromSeq != nil {
		return sql.NullInt64{Int64: *payload.HashV2FromSeq, Valid: true}
	}
	next := payload.LastSeq + 1
	if payload.LastHash == "" {
		next = 0 // an empty chain
	}
	if head.Empty || head.LastIndex < next {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: next, Valid: true}
}

func describeAnchoredHashV2Start(payload jobaudit.AnchorPayload, expected sql.NullInt64) string {
	switch {
	case payload.HashV2FromSeq != nil:
		return fmt.Sprintf("records hash v2 starting at event %d", *payload.HashV2FromSeq)
	case payload.LastHash == "":
		return "records an empty chain, so every event after it is hash v2"
	case expected.Valid:
		return fmt.Sprintf("records no hash v2 events up to event %d, so hash v2 starts at event %d",
			payload.LastSeq, expected.Int64)
	default:
		return fmt.Sprintf("records no hash v2 events up to event %d", payload.LastSeq)
	}
}

// checkAnchoredEvent checks the anchored event is in the database with the anchored hash.
func (w *anchorWalker) checkAnchoredEvent(ctx context.Context, record *anchorRecord) error {
	payload := record.payload
	if payload.LastHash == "" || (payload.LastSeq == w.verifiedSeq && payload.LastHash == w.verifiedHash) {
		return nil
	}
	row, err := w.source.AuditChainRowAt(ctx, payload.LastSeq)
	if err != nil {
		return err
	}
	switch {
	case row == nil:
		w.report.fail("anchor %s: anchored event %d is missing from the database", record.key, payload.LastSeq)
	case !strings.EqualFold(hex.EncodeToString(row.EventHash), payload.LastHash):
		w.report.fail("anchor %s: the hash of event %d doesn't match the anchor (the event or its hash changed)",
			record.key, payload.LastSeq)
	default:
		w.verifiedSeq, w.verifiedHash = payload.LastSeq, payload.LastHash
	}
	return nil
}

// reportMissingHours warns about hours without an anchor, from the first anchor up to the last full hour.
func reportMissingHours(anchored map[time.Time]bool, now time.Time, report *Report) {
	if len(anchored) == 0 {
		return
	}
	var first time.Time
	for hour := range anchored {
		if first.IsZero() || hour.Before(first) {
			first = hour
		}
	}
	last := now.UTC().Truncate(time.Hour).Add(-time.Hour)
	count := 0
	var examples []string
	for hour := first; !hour.After(last); hour = hour.Add(time.Hour) {
		if anchored[hour] {
			continue
		}
		count++
		if len(examples) < maxListedMissingHours {
			examples = append(examples, hour.Format("2006-01-02T15:00Z"))
		}
	}
	if count > 0 {
		report.warn("%d hours since the first anchor have no anchor (e.g. %s)", count, strings.Join(examples, ", "))
	}
}

// reportNoAnchors fails when the database has events but the bucket has no anchors: anchoring isn't running, or
// the bucket or chain ID is wrong. An empty chain only gets a warning (a new gateway before its first anchor).
func reportNoAnchors(opts AnchorOptions, head db.AuditChainHead, report *Report) {
	if head.Empty {
		report.warn("no anchors found under s3://%s/%s/", opts.Bucket, opts.ChainID)
		return
	}
	report.fail("no anchors found under s3://%s/%s/, but the database has events up to %d: anchors aren't being "+
		"written, or AUDIT_ANCHOR_BUCKET or AUDIT_ANCHOR_CHAIN_ID is wrong", opts.Bucket, opts.ChainID, head.LastIndex)
}

// checkLatestAnchorAge fails when the latest anchor was written more than maxAge before now: anchors have stopped
// being written, so nothing outside the database pins new events.
func checkLatestAnchorAge(latest *anchorRecord, maxAge time.Duration, now time.Time, report *Report) {
	report.LatestAnchor = latest.key
	report.LatestAnchorWritten = latest.lastModified
	if maxAge == 0 {
		maxAge = DefaultMaxAnchorAge
	}
	if age := now.Sub(latest.lastModified); age > maxAge {
		report.fail("the latest anchor (%s) was written %s ago, more than the maximum anchor age (%s): anchors "+
			"have stopped being written", latest.key, age.Truncate(time.Minute), maxAge)
	}
}

// reportUnanchoredTail warns about events no anchor covers yet: nothing outside the database pins them.
func reportUnanchoredTail(head db.AuditChainHead, maxSeq int64, report *Report) {
	if head.Empty || head.LastIndex <= maxSeq {
		return
	}
	report.warn("events %d..%d (%d) were written after the latest anchor and aren't anchored yet",
		maxSeq+1, head.LastIndex, head.LastIndex-maxSeq)
}
