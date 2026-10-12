package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ErrUnexpectedAnchor is returned when an hour's anchor key already holds an object the anchor writer didn't
// write. Someone with write access to the bucket put it there first, and while Object Lock keeps it that hour
// can't be anchored. Doing that every hour would stop anchoring without anything looking wrong but a growing
// unanchored tail, so the writer checks every existing anchor it finds.
var ErrUnexpectedAnchor = errors.New("an object the anchor writer didn't write holds this hour's anchor key")

// checkExistingAnchor accepts the anchor already stored for the hour of ts if the anchor writer could have
// written it: S3 stored it no earlier than the hour began, it is an anchor for that hour, and it records every
// event written before it was stored. A stale copy of an earlier anchor fails the last check. Event timestamps
// come from the gateway's clock and are set just before the event is written; AnchorClockSkew covers that and
// the difference from S3's clock.
func (w *AnchorWriterWorker) checkExistingAnchor(
	ctx context.Context,
	lock *db.AuditAnchorTx,
	key string,
	ts, written time.Time,
) error {
	if written.Before(ts.UTC().Truncate(time.Hour).Add(-AnchorClockSkew)) {
		return unexpectedAnchor(key, written, "before its hour began")
	}
	body, err := w.readAnchor(ctx, key)
	if err != nil {
		return err
	}
	var existing AnchorPayload
	if err := json.Unmarshal(body, &existing); err != nil {
		return unexpectedAnchor(key, written, "and it isn't anchor JSON")
	}
	if existingTS, err := time.Parse(time.RFC3339, existing.TimestampUTC); err != nil ||
		existing.ChainID != w.chainID || AnchorKey(w.chainID, existingTS) != key {
		return unexpectedAnchor(key, written, fmt.Sprintf("and it's for chain %q at %q, not this hour",
			existing.ChainID, existing.TimestampUTC))
	}
	covered, found, err := lock.LastIndexWrittenBy(ctx, written.Add(-AnchorClockSkew))
	if err != nil {
		return err
	}
	if found && (existing.LastHash == "" || existing.LastSeq < covered) {
		return unexpectedAnchor(key, written, fmt.Sprintf("but it doesn't record event %d, written before then "+
			"(a stale copy of an earlier anchor?)", covered))
	}
	return nil
}

func unexpectedAnchor(key string, written time.Time, reason string) error {
	return fmt.Errorf("%w: %s was stored at %s %s (check who can write to the bucket)", ErrUnexpectedAnchor, key,
		written.UTC().Format(time.RFC3339), reason)
}
