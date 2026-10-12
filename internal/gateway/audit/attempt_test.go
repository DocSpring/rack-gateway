package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func appDeleteEntry() *db.AuditLog {
	return &db.AuditLog{
		UserEmail:    "admin@example.com",
		ActionType:   "convox",
		Action:       "app.delete",
		ResourceType: "app",
		Resource:     "myapp",
	}
}

func TestRecordAttemptPairsTheAttemptWithItsResults(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)
	req := httptest.NewRequest(http.MethodDelete, "/apps/myapp", nil)

	ctx, err := logger.RecordAttempt(req, appDeleteEntry())
	require.NoError(t, err)
	attempt := AttemptFromContext(ctx)
	require.NotNil(t, attempt)
	require.NotEmpty(t, attempt.RequestID)
	require.Equal(t, attempt.RequestID, req.Header.Get("X-Request-ID"), "later entries read the same request ID")

	result := appDeleteEntry()
	result.Status = StatusSuccess
	result.HTTPStatus = http.StatusOK
	require.NoError(t, logger.LogRequestEntry(ctx, result))
	require.Equal(t, 1, attempt.Results())

	logs, err := database.GetAuditLogs("admin@example.com", time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	statuses := map[string]*db.AuditLog{}
	for _, entry := range logs {
		require.Equal(t, attempt.RequestID, entry.RequestID, "%s entry", entry.Status)
		statuses[entry.Status] = entry
	}
	require.Equal(t, attempt.ID, statuses[StatusAttempted].ID)
	require.Equal(t, "allow", statuses[StatusAttempted].RBACDecision)
	require.Contains(t, statuses, StatusSuccess)
}

func TestRecordAttemptFailsClosed(t *testing.T) {
	logger := NewLogger(nil)
	logger.SetChainWriter(audittest.FailingChainWriter{})
	req := httptest.NewRequest(http.MethodDelete, "/apps/myapp", nil)

	ctx, err := logger.RecordAttempt(req, appDeleteEntry())
	require.ErrorIs(t, err, ErrAuditUnavailable)
	require.ErrorIs(t, err, audittest.ErrUnavailable)
	require.Nil(t, AttemptFromContext(ctx), "a refused request has no attempt")
	require.Equal(t, "audit log unavailable; refusing to perform app.delete", RefusalMessage("app.delete"))
}

func TestEntriesWithoutAnAttemptAreNotLinked(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

	entry := appDeleteEntry()
	entry.Status = StatusSuccess
	require.NoError(t, logger.LogRequestEntry(context.Background(), entry))
	require.Empty(t, entry.RequestID)
}

// recordingWriter stores entries in memory, numbering them like the database does.
type recordingWriter struct {
	entries []*db.AuditLog
}

func (w *recordingWriter) CreateAuditLog(entry *db.AuditLog) error {
	entry.ID = int64(len(w.entries) + 1)
	w.entries = append(w.entries, entry)
	return nil
}

type countingEnqueuer struct {
	ids []int64
}

func (e *countingEnqueuer) EnqueueAuditEvent(auditLogID int64) error {
	e.ids = append(e.ids, auditLogID)
	return nil
}

// TestOnlyResultsAreSentToSlack checks an attempt and its result notify Slack once, for the result.
func TestOnlyResultsAreSentToSlack(t *testing.T) {
	writer := &recordingWriter{}
	enqueuer := &countingEnqueuer{}
	logger := NewLogger(nil)
	logger.SetChainWriter(writer)
	logger.SetAuditEventEnqueuer(enqueuer)
	req := httptest.NewRequest(http.MethodDelete, "/apps/myapp", nil)

	ctx, err := logger.RecordAttempt(req, appDeleteEntry())
	require.NoError(t, err)
	require.Empty(t, enqueuer.ids, "attempts aren't sent to Slack")

	result := appDeleteEntry()
	result.Status = StatusSuccess
	require.NoError(t, logger.LogRequestEntry(ctx, result))
	require.Equal(t, []int64{result.ID}, enqueuer.ids)
	require.Len(t, writer.entries, 2)
}
