package auditverify_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

const (
	testBucket  = "anchors"
	testChainID = "production"
)

var firstHour = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

// writeAnchor runs the real anchor writer for the hour of at.
func writeAnchor(t *testing.T, database *gwdb.Database, bucket *fakes3.Bucket, at time.Time) {
	t.Helper()
	worker := jobaudit.NewAnchorWriterWorker(database, bucket, testBucket, testChainID, 7)
	job := &river.Job[jobaudit.AnchorWriterArgs]{JobRow: &rivertype.JobRow{CreatedAt: at}}
	require.NoError(t, worker.Work(context.Background(), job))
}

// putAnchor stores an anchor for the hour of ts, written at ts, and returns its body.
func putAnchor(t *testing.T, bucket *fakes3.Bucket, payload jobaudit.AnchorPayload, ts time.Time) []byte {
	t.Helper()
	payload.ChainID = testChainID
	payload.TimestampUTC = ts.Format(time.RFC3339)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	bucket.PutAt(jobaudit.AnchorKey(testChainID, ts), body, ts)
	return body
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func verifyWithAnchors(
	t *testing.T,
	database *gwdb.Database,
	bucket *fakes3.Bucket,
	now time.Time,
) *auditverify.Report {
	t.Helper()
	return verify(t, database, auditverify.Options{
		Anchors: &auditverify.AnchorOptions{Store: bucket, Bucket: testBucket, ChainID: testChainID},
		Now:     now,
	})
}

func TestVerifyAnchorsWrittenByTheAnchorWriter(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 3)
	writeAnchor(t, database, bucket, firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour.Add(time.Hour))

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(2*time.Hour))
	requireOK(t, report)
	assert.Equal(t, 2, report.AnchorsChecked)
	assert.Equal(t, 0, report.WarningCount, "warnings: %q", report.Warnings)
	assert.Equal(t, jobaudit.AnchorKey(testChainID, firstHour.Add(time.Hour)), report.LatestAnchor)
}

func TestVerifyWarnsAboutUnanchoredEventsAndMissingHours(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour)
	writeEvents(t, database, 3)

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(4*time.Hour))
	requireOK(t, report)
	assert.Equal(t, 2, report.WarningCount, "warnings: %q", report.Warnings)
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "events 2..4 (3) were written after the latest anchor")
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "3 hours since the first anchor have no anchor")
}

func TestVerifyDetectsAnAnchorThatDoesNotMatchTheEvent(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 3)
	putAnchor(t, bucket, jobaudit.AnchorPayload{
		LastSeq:       1,
		LastHash:      strings.Repeat("00", 32),
		Format:        jobaudit.AnchorFormat,
		HashV2FromSeq: int64Ptr(0),
	}, firstHour)

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour))
	requireFailure(t, report, "the hash of event 1 doesn't match the anchor")
}

func TestVerifyDetectsEventsRemovedFromTheEnd(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 4)
	writeAnchor(t, database, bucket, firstHour)
	// The row checks can't see a shortened chain; the anchor can.
	tamper(t, database, `DELETE FROM audit.audit_event WHERE chain_index = 3`)

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour))
	requireFailure(t, report, "anchored event 3 is missing")
}

func TestVerifyDetectsAnOverwrittenAnchor(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour)
	key := jobaudit.AnchorKey(testChainID, firstHour)
	original := bucket.Versions(key)[0]
	bucket.PutAt(key, original, firstHour.Add(30*time.Minute)) // same content, new version

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour))
	requireFailure(t, report, key, "has 2 versions")
}

func TestVerifyDetectsADeletedAnchor(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour)
	key := jobaudit.AnchorKey(testChainID, firstHour)
	bucket.AddDeleteMarker(key)

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour))
	requireFailure(t, report, key, "was deleted")
	assert.Equal(t, 1, report.AnchorsChecked, "the locked version is still checked")
}

func TestVerifyDetectsV2EventsRelabeledAsLegacy(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 4)
	writeAnchor(t, database, bucket, firstHour)
	// Relabeling the first events moves where hash v2 seems to start, so the rows alone look consistent.
	tamper(t, database, `UPDATE audit.audit_event SET hash_version = 1, details = '{"n":9}' WHERE chain_index <= 1`)

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(time.Hour))
	requireFailure(t, report, "records hash v2 starting at event 0, but the database has hash v2 from event 2")
	assert.Equal(t, 1, report.FailureCount, "failures: %q", report.Failures)
}

func TestVerifyFollowsAnchorsPastTheFirstPage(t *testing.T) {
	database := newAuditDatabase(t) // no events: every anchor records an empty chain
	bucket := fakes3.New(firstHour)
	bucket.PageSize = 250
	const count = 1100
	var previous []byte
	var brokenKey string
	for i := 0; i < count; i++ {
		ts := firstHour.Add(time.Duration(i) * time.Hour)
		payload := jobaudit.AnchorPayload{Format: jobaudit.AnchorFormat}
		if previous != nil {
			payload.PrevAnchorHash = hashHex(previous)
		}
		if i == 1050 {
			payload.PrevAnchorHash = strings.Repeat("ab", 32)
			brokenKey = jobaudit.AnchorKey(testChainID, ts)
		}
		previous = putAnchor(t, bucket, payload, ts)
	}

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(count*time.Hour))
	assert.Equal(t, count, report.AnchorsChecked)
	requireFailure(t, report, brokenKey, "prev_anchor_hash doesn't match")
	assert.Equal(t, 1, report.FailureCount, "failures: %q", report.Failures)
}

func TestVerifyToleratesStaleLinksInLegacyAnchors(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	// Before anchor format 2 the writer only listed the first 1000 anchors, so later anchors all pointed at the
	// same one.
	first := putAnchor(t, bucket, jobaudit.AnchorPayload{}, firstHour)
	stale := hashHex(first)
	for i := 1; i < 4; i++ {
		putAnchor(t, bucket, jobaudit.AnchorPayload{PrevAnchorHash: stale}, firstHour.Add(time.Duration(i)*time.Hour))
	}

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(4*time.Hour))
	requireOK(t, report)
	assert.Equal(t, 2, report.LegacyAnchorLinkMismatches)
}
