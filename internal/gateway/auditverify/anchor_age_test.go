package auditverify_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

func TestVerifyFailsWhenTheLatestAnchorIsTooOld(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour)
	fourHoursLater := firstHour.Add(4 * time.Hour)

	// Anchors stopped being written after the first hour.
	report := verifyWithAnchors(t, database, bucket, fourHoursLater)
	requireFailure(t, report, "the latest anchor ("+report.LatestAnchor+") was written 3h59m0s ago",
		"more than the maximum anchor age (3h0m0s)")
	var summary bytes.Buffer
	require.NoError(t, report.Write(&summary))
	assert.Contains(t, summary.String(), "(written 2026-05-01T00:00:01Z)")

	// A gateway that anchors less often can be verified with a longer maximum.
	report = verify(t, database, auditverify.Options{
		Anchors: &auditverify.AnchorOptions{
			Store: bucket, Bucket: testBucket, ChainID: testChainID, MaxAge: 5 * time.Hour,
		},
		Now: fourHoursLater,
	})
	requireOK(t, report)
}

func TestVerifyFailsWhenThereAreEventsButNoAnchors(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)

	// A new gateway before its first anchor.
	report := verifyWithAnchors(t, database, bucket, firstHour)
	requireOK(t, report)
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "no anchors found")

	writeEvents(t, database, 2)
	report = verifyWithAnchors(t, database, bucket, firstHour)
	requireFailure(t, report, "no anchors found under s3://anchors/production/, but the database has events up to 1")
}

func TestVerifyFailsWhenAnchorsStopCoveringNewEvents(t *testing.T) {
	database := newAuditDatabase(t)
	start := time.Now().UTC().Truncate(time.Hour)
	bucket := fakes3.New(start)
	writeAnchor(t, database, bucket, start)
	writeEvents(t, database, 2) // written now, after the anchor

	// Within the maximum anchor age they're just the latest events.
	report := verifyWithAnchors(t, database, bucket, time.Now().Add(time.Hour))
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "events 0..1 (2) were written after the latest anchor")

	report = verifyWithAnchors(t, database, bucket, time.Now().Add(4*time.Hour))
	requireFailure(t, report, "events 0..1 (2) aren't anchored, and the oldest was written at",
		"more than the maximum anchor age (3h0m0s) ago")
}
