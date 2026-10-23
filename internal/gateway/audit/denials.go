package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// denialWindow is how long repeats of a denial are counted rather than written.
const denialWindow = time.Minute

// maxDenialRuns caps the runs kept. Past it, a denial without a run of its own shares one with its principal's other
// denials (see overflowKey), so a flood of distinct targets or client addresses can't grow memory or the log without
// bound.
const maxDenialRuns = 1000

// maxDenialKeyPath caps how much of a denied request's path its run's key keeps.
const maxDenialKeyPath = 512

// anonymousOverflowKey is the run the denials of requests without a principal share once maxDenialRuns is reached.
const anonymousOverflowKey = "ip:*"

// RecordDenial records a refused request. Denials are throttled so repeated attempts can't flood the log: the
// first denial of an action against a target for a user (or API token, or for a request without either, its
// client IP) is written at once, and its repeats in the next minute are written as one entry when the minute is
// up, with event_count saying how many there were.
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
// principal's denials of the same action against the same target (denialKey) until the window is up are only
// counted, and then one entry records them. The next denial after that starts a new run.
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

// denialRun is one window of a principal's denials of one action against one target.
type denialRun struct {
	started time.Time
	repeats int
	// last is the most recent repeat. The entry counting the repeats takes its fields.
	last        *db.AuditLog
	firstRepeat time.Time
	lastRepeat  time.Time
	ended       bool
	// overflow is the details field that marks a run shared past maxDenialRuns: from_many_clients for requests
	// without a principal, many_targets for one principal's. Empty for any other run.
	overflow string
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
	principal := denialPrincipal(entry)
	key := denialKey(principal, entry)
	now := t.now()

	t.mu.Lock()
	run := t.runs[key]
	overflow := ""
	if run == nil && len(t.runs) >= maxDenialRuns {
		key, overflow = overflowKey(principal)
		run = t.runs[key]
	}
	if run != nil && now.Sub(run.started) < t.window {
		run.countRepeat(entry, now)
		t.mu.Unlock()
		return
	}
	// The previous run's window is up but its timer hasn't fired yet: end it here, in order.
	expired := t.endLocked(key, run)
	next := &denialRun{started: now, overflow: overflow}
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
	fields := map[string]interface{}{
		"repeats_from":  run.firstRepeat.UTC().Format(time.RFC3339),
		"repeats_until": run.lastRepeat.UTC().Format(time.RFC3339),
	}
	if run.overflow != "" {
		// The repeats came from many clients, or were against many targets; the entry has the last one's fields.
		fields[run.overflow] = true
	}
	entry.Details = WithDetails(entry.Details, fields)
	t.write(&entry)
}

// denialPrincipal identifies who was denied: an API token, or else a user, or else the client IP.
func denialPrincipal(entry *db.AuditLog) string {
	switch {
	case entry.APITokenID != nil:
		return fmt.Sprintf("token:%d", *entry.APITokenID)
	case entry.UserEmail != "":
		return entry.UserEmail
	default:
		return "ip:" + entry.IPAddress
	}
}

// denialKey identifies a denial's principal, its action and its target: the resource, and for a proxied request its
// method and path (the first maxDenialKeyPath bytes). Only repeats against the same target are counted together, so
// a denial against one target can't hide among denials against others.
func denialKey(principal string, entry *db.AuditLog) string {
	method, path := detailsTarget(entry.Details)
	if len(path) > maxDenialKeyPath {
		path = path[:maxDenialKeyPath]
	}
	return strings.Join([]string{
		principal, entry.Action, entry.ResourceType, entry.Resource, entry.Command, method, path,
	}, "\x00")
}

// overflowKey is the run a principal's denials share once maxDenialRuns is reached, and the details field that
// marks it: requests without a principal share one run whatever their client, and each user or API token has one
// for all their denials.
func overflowKey(principal string) (key, field string) {
	if strings.HasPrefix(principal, "ip:") {
		return anonymousOverflowKey, "from_many_clients"
	}
	return principal + "\x00*", "many_targets"
}

// detailsTarget returns the method and path a proxied request's details name, if any.
func detailsTarget(details string) (method, path string) {
	if details == "" {
		return "", ""
	}
	var fields struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(details), &fields); err != nil {
		return "", ""
	}
	return fields.Method, fields.Path
}
