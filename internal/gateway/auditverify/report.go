package auditverify

import (
	"fmt"
	"io"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// maxMessages caps how many failures (and warnings) are kept for the summary; all of them are counted.
const maxMessages = 100

// maxFailureKeys caps how many failure keys a report keeps (see FailureKeys).
const maxFailureKeys = 1000

// Report is the outcome of a verification run.
type Report struct {
	// Head is the end of the chain when the run started.
	Head db.AuditChainHead

	EventsChecked int64
	FirstChecked  int64
	LastChecked   int64
	// LegacyEvents were written before hash v2: only their previous_hash links are checked. Their content can't be
	// verified: a v1 event_hash can't be recomputed, so a v1 event whose content was changed (by someone able to get
	// past the append-only trigger) still passes. LastLegacy is the last one checked.
	LegacyEvents int64
	LastLegacy   int64
	// V2Events had their HMAC recomputed.
	V2Events int64
	// LegacyIndexGaps are jumps in legacy chain indexes, which the old sequence-based writer left after failed
	// inserts. Their links are still checked.
	LegacyIndexGaps int64
	// HashV2FromPinned is the hash v2 start pinned with --hash-v2-from or AUDIT_HASH_V2_FROM (nil: not pinned).
	HashV2FromPinned *int64
	// HashV2StartAnchors counts the format 2 anchors checked, each of which records where hash v2 starts.
	HashV2StartAnchors int

	// AnchorsChecked counts anchor versions read from the bucket (0 when anchors weren't checked).
	AnchorsChecked int
	// LatestAnchor is the key of the most recently written anchor, and LatestAnchorWritten when S3 stored it.
	LatestAnchor        string
	LatestAnchorWritten time.Time
	// LegacyAnchorLinkMismatches counts anchors written before anchor format 2 whose prev_anchor_hash doesn't
	// name the anchor written before them (earlier versions only listed the first 1000 anchors).
	LegacyAnchorLinkMismatches int

	Failures     []string
	FailureCount int
	Warnings     []string
	WarningCount int
	// StoppedBy is why the run stopped before it finished ("" when it finished): the report holds what it found
	// until then.
	StoppedBy string

	// failureKeys identify the failures found (the first maxFailureKeys of them).
	failureKeys []string
}

// OK reports whether no integrity failure was found.
func (r *Report) OK() bool {
	return r.FailureCount == 0
}

func (r *Report) fail(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	r.addFailure(message, message)
}

// failSteady records a failure whose message changes from run to run (an age, or the latest event's index) under
// key, which doesn't: finding it again isn't a new failure.
func (r *Report) failSteady(key, format string, args ...interface{}) {
	r.addFailure(fmt.Sprintf(format, args...), key)
}

func (r *Report) addFailure(message, key string) {
	r.FailureCount++
	if len(r.Failures) < maxMessages {
		r.Failures = append(r.Failures, message)
	}
	if len(r.failureKeys) < maxFailureKeys {
		r.failureKeys = append(r.failureKeys, key)
	}
}

// FailureKeys identifies the failures found, up to maxFailureKeys of them. A failure found again by a later run has
// the same key, so a failure that wasn't found before stands out even when the count stays the same.
func (r *Report) FailureKeys() []string {
	return r.failureKeys
}

func (r *Report) warn(format string, args ...interface{}) {
	r.WarningCount++
	if len(r.Warnings) < maxMessages {
		r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
	}
}

func (r *Report) countEvent(row *db.AuditChainRow) {
	if r.EventsChecked == 0 {
		r.FirstChecked = row.ChainIndex
	}
	r.EventsChecked++
	r.LastChecked = row.ChainIndex
	if row.HashVersion == db.AuditHashVersion {
		r.V2Events++
	} else {
		r.LegacyEvents++
		r.LastLegacy = row.ChainIndex
	}
}

// Write prints a summary.
func (r *Report) Write(w io.Writer) error {
	p := &summaryPrinter{w: w}
	r.writeChain(p)
	r.writeAnchors(p)
	p.messages("Warnings", r.WarningCount, r.Warnings)
	p.messages("FAILURES", r.FailureCount, r.Failures)
	switch {
	case r.StoppedBy != "" && r.OK():
		p.line("Result: INCOMPLETE (stopped before it finished: %s)", r.StoppedBy)
	case r.StoppedBy != "":
		p.line("Result: FAILED (%d integrity failures, and stopped before it finished: %s)", r.FailureCount,
			r.StoppedBy)
	case r.OK() && r.LegacyEvents > 0:
		p.line("Result: OK, except that the content of %d legacy (v1) events can't be verified", r.LegacyEvents)
	case r.OK():
		p.line("Result: OK")
	default:
		p.line("Result: FAILED (%d integrity failures)", r.FailureCount)
	}
	return p.err
}

func (r *Report) writeChain(p *summaryPrinter) {
	switch {
	case r.Head.Empty:
		p.line("Audit chain: no events")
	case r.EventsChecked == 0:
		p.line("Audit chain: latest event %d; no events in the requested range", r.Head.LastIndex)
	default:
		p.line("Audit chain: checked %d events (%d..%d), latest event %d",
			r.EventsChecked, r.FirstChecked, r.LastChecked, r.Head.LastIndex)
	}
	if r.Head.HashV2From.Valid {
		p.line("  hash v2 from event %d: %d events checked, HMAC recomputed", r.Head.HashV2From.Int64, r.V2Events)
	}
	if r.LegacyEvents > 0 {
		p.line("  legacy (v1) events: %d checked (up to event %d), previous_hash links only: their content is "+
			"unverifiable (a v1 event_hash can't be recomputed)", r.LegacyEvents, r.LastLegacy)
	}
	if hasLegacyEvents(r.Head) {
		p.line("  hash v2 start: %s", r.describeHashV2StartConfirmation())
	}
	if r.LegacyIndexGaps > 0 {
		p.line("  legacy chain index gaps (left by failed inserts before hash v2): %d", r.LegacyIndexGaps)
	}
}

func (r *Report) writeAnchors(p *summaryPrinter) {
	if r.AnchorsChecked == 0 {
		return
	}
	p.line("Anchors: %d checked, latest %s (written %s)",
		r.AnchorsChecked, r.LatestAnchor, r.LatestAnchorWritten.UTC().Format(time.RFC3339))
	if r.LegacyAnchorLinkMismatches > 0 {
		p.line("  anchors from before anchor format 2 with a stale prev_anchor_hash: %d (known old bug, not a failure)",
			r.LegacyAnchorLinkMismatches)
	}
}

// describeHashV2StartConfirmation says what outside the database confirmed where hash v2 starts.
func (r *Report) describeHashV2StartConfirmation() string {
	switch {
	case r.HashV2FromPinned != nil && r.HashV2StartAnchors > 0:
		return fmt.Sprintf("pinned at event %d, and recorded by %d format 2 anchors",
			*r.HashV2FromPinned, r.HashV2StartAnchors)
	case r.HashV2FromPinned != nil:
		return fmt.Sprintf("pinned at event %d", *r.HashV2FromPinned)
	case r.HashV2StartAnchors > 0:
		return fmt.Sprintf("recorded by %d format 2 anchors", r.HashV2StartAnchors)
	default:
		return "NOT confirmed outside the database (pin it, see the failure below)"
	}
}

// summaryPrinter keeps the first write error.
type summaryPrinter struct {
	w   io.Writer
	err error
}

func (p *summaryPrinter) line(format string, args ...interface{}) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format+"\n", args...)
	}
}

func (p *summaryPrinter) messages(title string, count int, messages []string) {
	if count == 0 {
		return
	}
	p.line("%s (%d):", title, count)
	for _, message := range messages {
		p.line("  - %s", message)
	}
	if count > len(messages) {
		p.line("  ... and %d more", count-len(messages))
	}
}
