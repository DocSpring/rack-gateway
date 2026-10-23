package auditverify_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// writeAnchor runs the real anchor writer for the hour of at. S3 stores the anchor a second after at.
func writeAnchor(t *testing.T, database *gwdb.Database, bucket *fakes3.Bucket, at time.Time) {
	t.Helper()
	writeAnchorStoredAt(t, database, bucket, at, at)
}

// writeAnchorStoredAt runs the real anchor writer for a job created at at, while S3's clock reads stored. S3
// stores the anchor a second after stored.
func writeAnchorStoredAt(t *testing.T, database *gwdb.Database, bucket *fakes3.Bucket, at, stored time.Time) {
	t.Helper()
	bucket.AdvanceTo(stored)
	worker := jobaudit.NewAnchorWriterWorker(database, bucket, testBucket, testChainID)
	job := &river.Job[jobaudit.AnchorWriterArgs]{JobRow: &rivertype.JobRow{CreatedAt: at}}
	require.NoError(t, worker.Work(context.Background(), job))
}

// putAnchor stores an anchor for the hour of ts, written at ts, and returns its body.
func putAnchor(t *testing.T, bucket *fakes3.Bucket, payload jobaudit.AnchorPayload, ts time.Time) []byte {
	t.Helper()
	return putAnchorAt(t, bucket, payload, ts, ts)
}

// putAnchorAt stores an anchor for the hour of ts that S3 stored at written, and returns its body.
func putAnchorAt(t *testing.T, bucket *fakes3.Bucket, payload jobaudit.AnchorPayload, ts, written time.Time) []byte {
	t.Helper()
	payload.ChainID = testChainID
	payload.TimestampUTC = ts.Format(time.RFC3339)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	bucket.PutAt(jobaudit.AnchorKey(testChainID, ts), body, written)
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

// S3 times are whole seconds. A delayed job for an earlier hour can store its anchor in the same second as a later
// hour's, linking to it: the verifier and the next write both follow the links, not the keys.
func TestVerifyAnchorsStoredInTheSameSecond(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 3)
	bucket.SameSecond = true
	stored := firstHour.Add(70 * time.Minute)
	writeAnchorStoredAt(t, database, bucket, firstHour.Add(time.Hour), stored)
	writeAnchorStoredAt(t, database, bucket, firstHour, stored)
	bucket.SameSecond = false
	writeEvents(t, database, 1)
	writeAnchor(t, database, bucket, firstHour.Add(2*time.Hour))

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(3*time.Hour))
	requireOK(t, report)
	assert.Equal(t, 3, report.AnchorsChecked)
}

func TestVerifyWarnsAboutUnanchoredEventsAndMissingHours(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour)
	writeAnchor(t, database, bucket, firstHour.Add(3*time.Hour)) // no anchors for two hours
	writeEvents(t, database, 3)

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(4*time.Hour))
	requireOK(t, report)
	assert.Equal(t, 2, report.WarningCount, "warnings: %q", report.Warnings)
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "events 2..4 (3) were written after the latest anchor")
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "2 hours since the first anchor have no anchor")
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

// barrierStore holds each anchor read until width reads are in flight at once, so the reads only finish when they
// run concurrently.
type barrierStore struct {
	*fakes3.Bucket
	width   int64
	started atomic.Int64
	release chan struct{}
}

func (s *barrierStore) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	if s.started.Add(1) == s.width {
		close(s.release)
	}
	select {
	case <-s.release:
		return s.Bucket.GetObject(ctx, params, optFns...)
	case <-time.After(10 * time.Second):
		return nil, errors.New("anchor reads didn't run concurrently")
	}
}

func TestVerifyReadsAnchorsConcurrently(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	const anchors = 4
	for hour := range anchors {
		writeEvents(t, database, 1)
		writeAnchor(t, database, bucket, firstHour.Add(time.Duration(hour)*time.Hour))
	}

	store := &barrierStore{Bucket: bucket, width: anchors, release: make(chan struct{})}
	report := verify(t, database, auditverify.Options{
		Anchors: &auditverify.AnchorOptions{Store: store, Bucket: testBucket, ChainID: testChainID},
		Now:     firstHour.Add(anchors * time.Hour),
	})
	requireOK(t, report)
	assert.Equal(t, anchors, report.AnchorsChecked)
}

// Anchors are only kept as long as the bucket's default Object Lock rule says, so verification fails when that's
// shorter than AUDIT_ANCHOR_RETENTION_DAYS.
func TestVerifyFailsWhenTheBucketKeepsAnchorsTooBriefly(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 1)
	writeAnchor(t, database, bucket, firstHour)
	bucket.LockConfiguration.Rule.DefaultRetention.Days = aws.Int32(30)

	report := verify(t, database, auditverify.Options{
		Anchors: &auditverify.AnchorOptions{
			Store: bucket, Bucket: testBucket, ChainID: testChainID, MinRetentionDays: 400,
		},
		Now: firstHour.Add(time.Hour),
	})
	require.Equal(t, 1, report.FailureCount, "failures: %q", report.Failures)
	assert.Contains(t, report.Failures[0], "locks anchors for 30 days, less than AUDIT_ANCHOR_RETENTION_DAYS (400)")
}

// readRecordingStore records the keys of the anchors it's asked for. listsSizes false drops the sizes from its
// listings, so only the read itself can tell an object is too big.
type readRecordingStore struct {
	*fakes3.Bucket
	listsSizes bool
	mu         sync.Mutex
	reads      []string
}

func (s *readRecordingStore) ListObjectVersions(
	ctx context.Context,
	params *s3.ListObjectVersionsInput,
	optFns ...func(*s3.Options),
) (*s3.ListObjectVersionsOutput, error) {
	output, err := s.Bucket.ListObjectVersions(ctx, params, optFns...)
	if err == nil && !s.listsSizes {
		for i := range output.Versions {
			output.Versions[i].Size = nil
		}
	}
	return output, err
}

func (s *readRecordingStore) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	s.mu.Lock()
	s.reads = append(s.reads, aws.ToString(params.Key))
	s.mu.Unlock()
	return s.Bucket.GetObject(ctx, params, optFns...)
}

// An object at an anchor's key that's bigger than any anchor is a failure. It isn't read when the listing shows its
// size, and the read stops at the limit when it doesn't: reading it all could exhaust the gateway's memory.
func TestVerifyRefusesOversizedAnchors(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 1)
	writeAnchor(t, database, bucket, firstHour)
	oversized := jobaudit.AnchorKey(testChainID, firstHour.Add(time.Hour))
	bucket.PutAt(oversized, make([]byte, 10*jobaudit.MaxAnchorSize), firstHour.Add(time.Hour))

	for _, listsSizes := range []bool{true, false} {
		store := &readRecordingStore{Bucket: bucket, listsSizes: listsSizes}
		report := verify(t, database, auditverify.Options{
			Anchors: &auditverify.AnchorOptions{Store: store, Bucket: testBucket, ChainID: testChainID},
			Now:     firstHour.Add(2 * time.Hour),
		})
		requireFailure(t, report, oversized, "larger than an anchor can be")
		if listsSizes {
			assert.NotContains(t, store.reads, oversized)
		} else {
			assert.Contains(t, store.reads, oversized)
		}
	}
}
