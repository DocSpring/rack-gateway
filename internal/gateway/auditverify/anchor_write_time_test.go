package auditverify_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

func TestVerifyFailsAnAnchorStoredBeforeItsTimestamp(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeAnchor(t, database, bucket, firstHour)
	// Someone with write access to the bucket creates a later hour's anchor ahead of time. The writer finds that
	// hour's key taken, so nothing would anchor it for as long as Object Lock keeps the object.
	later := firstHour.Add(5 * time.Hour)
	putAnchorAt(t, bucket, jobaudit.AnchorPayload{Format: jobaudit.AnchorFormat}, later, firstHour.Add(time.Hour))

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(2*time.Hour))
	requireFailure(t, report, jobaudit.AnchorKey(testChainID, later), "was stored at 2026-05-01T01:00:00Z",
		"before its ts_utc (2026-05-01T05:00:00Z): it was created ahead of time")
}

func TestVerifyFailsAnAnchorStoredLongAfterItsTimestamp(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeAnchor(t, database, bucket, firstHour)
	// A missing hour backfilled a day later.
	backfilled := firstHour.Add(time.Hour)
	putAnchorAt(t, bucket, jobaudit.AnchorPayload{Format: jobaudit.AnchorFormat}, backfilled,
		backfilled.Add(24*time.Hour))

	report := verifyWithAnchors(t, database, bucket, backfilled.Add(25*time.Hour))
	requireFailure(t, report, jobaudit.AnchorKey(testChainID, backfilled), "was stored 24h0m0s after its ts_utc",
		"it was backfilled")
}

func TestVerifyWarnsAboutLateLegacyAnchors(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	// Before anchor format 2, a failing job was retried for weeks, so a late anchor may be genuine.
	putAnchorAt(t, bucket, jobaudit.AnchorPayload{}, firstHour, firstHour.Add(2*24*time.Hour))

	report := verifyWithAnchors(t, database, bucket, firstHour.Add(2*24*time.Hour+time.Hour))
	requireOK(t, report)
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "was stored 48h0m0s after its ts_utc")
}

func TestVerifyAllowsClockSkewAndRetriesWithinTheWriteWindow(t *testing.T) {
	database := newAuditDatabase(t)
	bucket := fakes3.New(firstHour)
	writeEvents(t, database, 2)
	writeAnchor(t, database, bucket, firstHour)
	// S3's clock two minutes behind the database's.
	nextHour := firstHour.Add(time.Hour)
	writeAnchorStoredAt(t, database, bucket, nextHour, nextHour.Add(-2*time.Minute))
	// A retry that succeeded at the end of the write window.
	retried := firstHour.Add(2 * time.Hour)
	writeAnchorStoredAt(t, database, bucket, retried, retried.Add(jobaudit.AnchorWriteWindow))

	requireOK(t, verifyWithAnchors(t, database, bucket, retried.Add(jobaudit.AnchorWriteWindow+time.Hour)))
}
