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
	if opts.MinRetentionDays > 0 {
		if err := CheckBucketRetention(ctx, opts.Store, opts.Bucket, opts.MinRetentionDays); err != nil {
			report.fail("%v", err)
		}
	}
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
	return checkUnanchoredTail(ctx, source, head, walker.maxSeq, anchorMaxAge(opts.MaxAge), now, report)
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
	// formatSince is when S3 stored the first anchor with the current format (zero before one is found).
	formatSince time.Time
}

// legacyAnchorGrace is how long after the first anchor in the current format an anchor without a format can still
// be stored: during the rolling deploy that brings in the format, an instance of the earlier version can write
// the next hour's anchor.
const legacyAnchorGrace = 24 * time.Hour

func (w *anchorWalker) check(ctx context.Context, record *anchorRecord) error {
	defer func() { w.prev = record }()
	if record.invalid {
		return nil
	}
	w.checkFormat(record)
	w.checkLinkToPrevious(record)
	w.checkIdentity(record)
	w.checkSequence(record)
	w.checkHashV2Start(record)
	return w.checkAnchoredEvent(ctx, record)
}

// checkFormat fails an anchor without a format that S3 stored more than legacyAnchorGrace after the first anchor
// in the current format: the anchor writer no longer writes one, so someone with write access to the bucket did,
// and the checks that make allowances for earlier versions' anchors mustn't excuse it. Anchors are checked in the
// order S3 stored them, so the first anchor in the current format has been seen by then.
func (w *anchorWalker) checkFormat(record *anchorRecord) {
	if record.payload.Format >= jobaudit.AnchorFormat {
		if w.formatSince.IsZero() {
			w.formatSince = record.lastModified
		}
		return
	}
	if !w.formatSince.IsZero() && record.lastModified.After(w.formatSince.Add(legacyAnchorGrace)) {
		w.report.fail("anchor %s has no format but was stored at %s, after anchors in format %d began (%s): the "+
			"anchor writer didn't write it", record.key, record.lastModified.UTC().Format(time.RFC3339),
			jobaudit.AnchorFormat, w.formatSince.UTC().Format(time.RFC3339))
	}
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

// checkIdentity checks the anchor belongs to this chain, sits under the key for its own timestamp and was stored
// when the anchor writer could have written it.
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
	w.checkWriteTime(record, ts)
	w.anchoredHour[ts.UTC().Truncate(time.Hour)] = true
}

// checkWriteTime compares when S3 stored the anchor with its ts_utc, when its job was created. The writer only
// writes after that, and gives up jobaudit.AnchorWriteWindow later (give or take jobaudit.AnchorClockSkew). An
// anchor stored before its ts_utc was created ahead of time by someone with write access to the bucket, which
// also stops the writer from anchoring that hour; one stored long after it was backfilled. Earlier versions
// retried a failing job for weeks, so a late anchor without a format is only a warning.
func (w *anchorWalker) checkWriteTime(record *anchorRecord, ts time.Time) {
	written := record.lastModified
	if written.Before(ts.Add(-jobaudit.AnchorClockSkew)) {
		w.report.fail("anchor %s was stored at %s, before its ts_utc (%s): it was created ahead of time, not by the "+
			"anchor writer (check who can write to the bucket, and to the database's job table)", record.key,
			written.UTC().Format(time.RFC3339), record.payload.TimestampUTC)
		return
	}
	late := written.Sub(ts)
	if late <= jobaudit.AnchorWriteWindow+jobaudit.AnchorClockSkew {
		return
	}
	message := fmt.Sprintf("anchor %s was stored %s after its ts_utc (%s), later than the anchor writer writes "+
		"(%s)", record.key, late.Truncate(time.Minute), record.payload.TimestampUTC, jobaudit.AnchorWriteWindow)
	if record.payload.Format >= jobaudit.AnchorFormat {
		w.report.fail("%s: it was backfilled", message)
		return
	}
	w.report.warn("%s; versions before anchor format %d retried a failing anchor job for weeks",
		message, jobaudit.AnchorFormat)
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
	maxAge = anchorMaxAge(maxAge)
	if age := now.Sub(latest.lastModified); age > maxAge {
		report.fail("the latest anchor (%s) was written %s ago, more than the maximum anchor age (%s): anchors "+
			"have stopped being written", latest.key, age.Truncate(time.Minute), maxAge)
	}
}

// anchorMaxAge is the maximum anchor age verification uses: maxAge, or DefaultMaxAnchorAge when it's 0.
func anchorMaxAge(maxAge time.Duration) time.Duration {
	if maxAge == 0 {
		return DefaultMaxAnchorAge
	}
	return maxAge
}

// checkUnanchoredTail reports events no anchor covers yet: nothing outside the database pins them. That's expected
// for the last hour's events, so it's a warning, unless the oldest of them was written more than maxAge ago:
// anchors are being written but don't cover new events (each hour's key taken first by a stale anchor, say).
func checkUnanchoredTail(
	ctx context.Context,
	source ChainSource,
	head db.AuditChainHead,
	maxSeq int64,
	maxAge time.Duration,
	now time.Time,
	report *Report,
) error {
	if head.Empty || head.LastIndex <= maxSeq {
		return nil
	}
	oldest, err := source.AuditChainRowAt(ctx, maxSeq+1)
	if err != nil {
		return fmt.Errorf("failed to read audit event %d: %w", maxSeq+1, err)
	}
	if oldest != nil && now.Sub(oldest.Timestamp) > maxAge {
		report.fail("events %d..%d (%d) aren't anchored, and the oldest was written at %s, more than the maximum "+
			"anchor age (%s) ago: anchors are being written but don't cover new events", maxSeq+1, head.LastIndex,
			head.LastIndex-maxSeq, oldest.Timestamp.UTC().Format(time.RFC3339), maxAge)
		return nil
	}
	report.warn("events %d..%d (%d) were written after the latest anchor and aren't anchored yet",
		maxSeq+1, head.LastIndex, head.LastIndex-maxSeq)
	return nil
}
