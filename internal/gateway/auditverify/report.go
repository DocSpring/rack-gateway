package auditverify

import (
	"fmt"
	"io"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// maxMessages caps how many failures (and warnings) are kept for the summary; all of them are counted.
const maxMessages = 100

// Report is the outcome of a verification run.
type Report struct {
	// Head is the end of the chain when the run started.
	Head db.AuditChainHead

	EventsChecked int64
	FirstChecked  int64
	LastChecked   int64
	// LegacyEvents were written before hash v2: only their previous_hash links are checked.
	LegacyEvents int64
	// V2Events had their HMAC recomputed.
	V2Events int64
	// LegacyIndexGaps are jumps in legacy chain indexes, which the old sequence-based writer left after failed
	// inserts. Their links are still checked.
	LegacyIndexGaps int64

	// AnchorsChecked counts anchor versions read from the bucket (0 when anchors weren't checked).
	AnchorsChecked int
	// LatestAnchor is the key of the most recently written anchor.
	LatestAnchor string
	// LegacyAnchorLinkMismatches counts anchors written before anchor format 2 whose prev_anchor_hash doesn't
	// name the anchor written before them (earlier versions only listed the first 1000 anchors).
	LegacyAnchorLinkMismatches int

	Failures     []string
	FailureCount int
	Warnings     []string
	WarningCount int
}

// OK reports whether no integrity failure was found.
func (r *Report) OK() bool {
	return r.FailureCount == 0
}

func (r *Report) fail(format string, args ...interface{}) {
	r.FailureCount++
	if len(r.Failures) < maxMessages {
		r.Failures = append(r.Failures, fmt.Sprintf(format, args...))
	}
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
	}
}

// Write prints a summary.
func (r *Report) Write(w io.Writer) error {
	p := &summaryPrinter{w: w}
	r.writeChain(p)
	r.writeAnchors(p)
	p.messages("Warnings", r.WarningCount, r.Warnings)
	p.messages("FAILURES", r.FailureCount, r.Failures)
	if r.OK() {
		p.line("Result: OK")
	} else {
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
		p.line("  legacy (v1) events: %d checked, previous_hash links only (HMAC not recomputable)", r.LegacyEvents)
	}
	if r.LegacyIndexGaps > 0 {
		p.line("  legacy chain index gaps (left by failed inserts before hash v2): %d", r.LegacyIndexGaps)
	}
}

func (r *Report) writeAnchors(p *summaryPrinter) {
	if r.AnchorsChecked == 0 {
		return
	}
	p.line("Anchors: %d checked, latest %s", r.AnchorsChecked, r.LatestAnchor)
	if r.LegacyAnchorLinkMismatches > 0 {
		p.line("  anchors from before anchor format 2 with a stale prev_anchor_hash: %d (known old bug, not a failure)",
			r.LegacyAnchorLinkMismatches)
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
