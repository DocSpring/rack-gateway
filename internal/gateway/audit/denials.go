package audit

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// denialWindow is how long repeats of a denial are counted rather than written.
const denialWindow = time.Minute

// RecordDenial records a refused request. Denials are throttled so repeated attempts can't flood the log: the
// first denial of an action for a user (or API token) is written at once, and its repeats in the next minute are
// written as one entry when the minute is up, with event_count saying how many there were.
//
// A denied request never acts, so it's refused whether or not its denial is written. A failed write is reported
// (logged, sent to Sentry and failing the health check) like any other.
func (l *Logger) RecordDenial(entry *db.AuditLog) {
	entry.Status = StatusDenied
	entry.RBACDecision = "deny"
	l.denials.record(entry)
}

// FlushDenials writes the repeat counts still being kept. The gateway calls it when it shuts down.
func (l *Logger) FlushDenials() {
	l.denials.flush()
}

// writeDenial writes a denial or a count of repeats. write has already reported a failure.
func (l *Logger) writeDenial(entry *db.AuditLog) {
	_ = l.LogDBEntry(entry)
}

// WithDetails returns details (a JSON object, or empty) with fields added. Details that aren't a JSON object
// are returned unchanged.
func WithDetails(details string, fields map[string]interface{}) string {
	merged := map[string]interface{}{}
	if details != "" {
		if err := json.Unmarshal([]byte(details), &merged); err != nil || merged == nil {
			return details
		}
	}
	for key, value := range fields {
		merged[key] = value
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return details
	}
	return string(data)
}

// denialThrottle collapses repeated denials. A run starts with a denial, which is written at once. The same
// principal's denials of the same action until the window is up are only counted, and then one entry records
// them. The next denial after that starts a new run.
//
// Runs are kept in memory. That's enough because a gateway runs as a single instance per rack (convox.yml scale
// count 1); a restart loses at most one window's count of repeats, never a first denial.
type denialThrottle struct {
	write    func(*db.AuditLog)
	window   time.Duration
	now      func() time.Time
	schedule func(delay time.Duration, f func())

	mu   sync.Mutex
	runs map[string]*denialRun
}

// denialRun is one window of a principal's denials of one action.
type denialRun struct {
	started time.Time
	repeats int
	// last is the most recent repeat. The entry counting the repeats takes its fields.
	last        *db.AuditLog
	firstRepeat time.Time
	lastRepeat  time.Time
	ended       bool
}

func newDenialThrottle(write func(*db.AuditLog)) *denialThrottle {
	return &denialThrottle{
		write:    write,
		window:   denialWindow,
		now:      time.Now,
		schedule: func(delay time.Duration, f func()) { time.AfterFunc(delay, f) },
		runs:     make(map[string]*denialRun),
	}
}

// record writes entry if it starts a run, or counts it as a repeat.
func (t *denialThrottle) record(entry *db.AuditLog) {
	key := denialKey(entry)
	now := t.now()

	t.mu.Lock()
	run := t.runs[key]
	if run != nil && now.Sub(run.started) < t.window {
		run.countRepeat(entry, now)
		t.mu.Unlock()
		return
	}
	// The previous run's window is up but its timer hasn't fired yet: end it here, in order.
	expired := t.endLocked(key, run)
	next := &denialRun{started: now}
	t.runs[key] = next
	t.mu.Unlock()

	t.schedule(t.window, func() { t.end(key, next) })
	t.writeRepeats(expired)
	entry.EventCount = 1
	t.write(entry)
}

func (r *denialRun) countRepeat(entry *db.AuditLog, at time.Time) {
	if r.repeats == 0 {
		r.firstRepeat = at
	}
	r.repeats++
	r.lastRepeat = at
	r.last = entry
}

// end ends run when its window is up and writes its repeats.
func (t *denialThrottle) end(key string, run *denialRun) {
	t.mu.Lock()
	ended := t.endLocked(key, run)
	t.mu.Unlock()
	t.writeRepeats(ended)
}

// endLocked marks run ended and forgets it, and returns it when it has repeats to write. t.mu must be held.
func (t *denialThrottle) endLocked(key string, run *denialRun) *denialRun {
	if run == nil || run.ended {
		return nil
	}
	run.ended = true
	if t.runs[key] == run {
		delete(t.runs, key)
	}
	if run.repeats == 0 {
		return nil
	}
	return run
}

// flush ends every run and writes their repeats.
func (t *denialThrottle) flush() {
	t.mu.Lock()
	ended := make([]*denialRun, 0, len(t.runs))
	for key, run := range t.runs {
		if withRepeats := t.endLocked(key, run); withRepeats != nil {
			ended = append(ended, withRepeats)
		}
	}
	t.mu.Unlock()
	for _, run := range ended {
		t.writeRepeats(run)
	}
}

// writeRepeats writes one entry for run's repeats: the last repeat's fields, with event_count set to how many
// there were and when they happened in its details.
func (t *denialThrottle) writeRepeats(run *denialRun) {
	if run == nil {
		return
	}
	entry := *run.last
	entry.EventCount = run.repeats
	entry.Details = WithDetails(entry.Details, map[string]interface{}{
		"repeats_from":  run.firstRepeat.UTC().Format(time.RFC3339),
		"repeats_until": run.lastRepeat.UTC().Format(time.RFC3339),
	})
	t.write(&entry)
}

// denialKey identifies the principal (an API token, or else a user) and the action a denial is about.
func denialKey(entry *db.AuditLog) string {
	principal := entry.UserEmail
	if entry.APITokenID != nil {
		principal = fmt.Sprintf("token:%d", *entry.APITokenID)
	}
	return principal + "\x00" + entry.Action
}
