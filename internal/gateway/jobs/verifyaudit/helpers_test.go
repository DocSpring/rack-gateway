package verifyaudit

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

const (
	testSecret  = "verify-job-test-secret"
	testBucket  = "anchors"
	testChainID = "production"
)

// firstRun is half past midnight, when the schedule runs.
var firstRun = time.Date(2026, 5, 1, 0, 30, 0, 0, time.UTC)

// harness runs the worker against a test database, with the gateway's audit logger recording alerts. Slack is
// connected with a channel for verification alerts; the queued Slack notifications and the Sentry events are kept.
type harness struct {
	t        *testing.T
	database *db.Database
	opts     auditverify.Options
	logger   *audit.Logger
	// bucket is nil when anchors aren't configured.
	bucket *fakes3.Bucket
	worker *Worker
	slack  *queuedSlackEvents
	// sentryEvents are the events sent to Sentry.
	sentryEvents []*sentry.Event
	// heartbeats counts the heartbeat pings; heartbeatErr is what they return.
	heartbeats   int
	heartbeatErr error
	now          time.Time
	nextUser     int
}

type queuedSlackEvents struct {
	ids []int64
}

func (q *queuedSlackEvents) EnqueueAuditEvent(auditLogID int64) error {
	q.ids = append(q.ids, auditLogID)
	return nil
}

func newHarness(t *testing.T, store auditverify.AnchorStore) *harness {
	t.Helper()
	t.Setenv("AUDIT_HMAC_SECRET", testSecret)
	h := &harness{t: t, database: dbtest.NewDatabase(t), slack: &queuedSlackEvents{}, now: firstRun}
	_, err := h.database.CreateSlackIntegration("T123", "Test", "xoxb-test-token", "U123", "chat:write",
		map[string]interface{}{"security": map[string]interface{}{
			"id": "C123", "name": "#security", "actions": []string{AlertAction},
		}}, nil)
	require.NoError(t, err)

	h.logger = audit.NewLogger(h.database)
	h.logger.SetAuditEventEnqueuer(h.slack)
	h.opts = auditverify.Options{Secret: []byte(testSecret)}
	if store != nil {
		h.opts.Anchors = &auditverify.AnchorOptions{Store: store, Bucket: testBucket, ChainID: testChainID}
	}
	h.restart()
	return h
}

// restart replaces the worker with a new one, as a gateway restart does.
func (h *harness) restart() {
	h.worker = NewWorker(h.database, h.opts, h.logger.LogDBEntry, func(context.Context) error {
		h.heartbeats++
		return h.heartbeatErr
	})
	h.worker.capture = func(event *sentry.Event) { h.sentryEvents = append(h.sentryEvents, event) }
	h.worker.now = func() time.Time { return h.now }
}

// newAnchoredHarness configures anchors in a fake bucket.
func newAnchoredHarness(t *testing.T) *harness {
	t.Helper()
	bucket := fakes3.New(firstRun.Add(-time.Hour))
	h := newHarness(t, bucket)
	h.bucket = bucket
	return h
}

// writeEvents adds count events as the gateway writes them, and returns their chain indexes.
func (h *harness) writeEvents(count int) []int64 {
	h.t.Helper()
	indexes := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		h.nextUser++
		log := &db.AuditLog{
			UserEmail:    fmt.Sprintf("user%d@example.com", h.nextUser),
			ActionType:   "convox",
			Action:       "app.list",
			Resource:     "web",
			Status:       "success",
			RBACDecision: "allow",
			HTTPStatus:   200,
		}
		require.NoError(h.t, h.database.CreateAuditLog(log))
		indexes = append(indexes, h.head())
	}
	return indexes
}

func (h *harness) head() int64 {
	h.t.Helper()
	head, err := h.database.AuditChainHead(context.Background())
	require.NoError(h.t, err)
	require.False(h.t, head.Empty)
	return head.LastIndex
}

// writeAnchor writes the anchor for the current hour with the real anchor writer.
func (h *harness) writeAnchor() {
	h.t.Helper()
	h.bucket.AdvanceTo(h.now)
	writer := jobaudit.NewAnchorWriterWorker(h.database, h.bucket, testBucket, testChainID, 7)
	require.NoError(h.t, writer.WriteAnchor(context.Background(), h.now))
}

// runAt runs the worker at now and returns the recorded status.
func (h *harness) runAt(now time.Time) *db.AuditVerificationStatus {
	h.t.Helper()
	h.now = now
	require.NoError(h.t, h.worker.Run(context.Background()))
	status, err := h.database.AuditVerificationStatus(context.Background())
	require.NoError(h.t, err)
	require.NotNil(h.t, status)
	return status
}

// slackAlerts returns the audit events queued for Slack.
func (h *harness) slackAlerts() []*db.AuditLog {
	h.t.Helper()
	alerts := make([]*db.AuditLog, 0, len(h.slack.ids))
	for _, id := range h.slack.ids {
		log, err := h.database.GetAuditLogByID(id)
		require.NoError(h.t, err)
		alerts = append(alerts, log)
	}
	return alerts
}

// tamper runs statements with the append-only triggers switched off, as someone with superuser access to the
// database could.
func (h *harness) tamper(statements ...string) {
	h.t.Helper()
	tx, err := h.database.DB().Begin()
	require.NoError(h.t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec("SET LOCAL session_replication_role = replica")
	require.NoError(h.t, err)
	for _, statement := range statements {
		_, err = tx.Exec(statement)
		require.NoError(h.t, err, statement)
	}
	require.NoError(h.t, tx.Commit())
}

// setAction changes an event's action in place.
func (h *harness) setAction(index int64, action string) {
	h.t.Helper()
	h.tamper(fmt.Sprintf(`UPDATE audit.audit_event SET action = '%s' WHERE chain_index = %d`, action, index))
}

// countingStore counts the anchor versions fetched from the bucket (concurrently).
type countingStore struct {
	*fakes3.Bucket
	gets atomic.Int64
}

func (c *countingStore) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	c.gets.Add(1)
	return c.Bucket.GetObject(ctx, params, optFns...)
}

// unreachableStore is a bucket the gateway can't list, as when its role lacks s3:ListBucketVersions.
type unreachableStore struct {
	*fakes3.Bucket
}

func (unreachableStore) ListObjectVersions(
	context.Context,
	*s3.ListObjectVersionsInput,
	...func(*s3.Options),
) (*s3.ListObjectVersionsOutput, error) {
	return nil, fmt.Errorf("AccessDenied: not allowed to list object versions")
}
