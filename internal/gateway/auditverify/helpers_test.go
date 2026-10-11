package auditverify_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const testSecret = "verify-test-secret"

// newAuditDatabase returns a fresh database whose events are written with testSecret.
func newAuditDatabase(t *testing.T) *gwdb.Database {
	t.Helper()
	t.Setenv("AUDIT_HMAC_SECRET", testSecret)
	return dbtest.NewDatabase(t)
}

func writeEvents(t *testing.T, database *gwdb.Database, count int) []*gwdb.AuditLog {
	t.Helper()
	logs := make([]*gwdb.AuditLog, 0, count)
	for i := 0; i < count; i++ {
		log := &gwdb.AuditLog{
			UserEmail:      "alice@example.com",
			UserName:       "Bob",
			ActionType:     "convox",
			Action:         "app.list",
			Resource:       fmt.Sprintf("app-%d", i),
			Details:        `{"n":1}`,
			IPAddress:      "203.0.113.10",
			UserAgent:      "verify-test",
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			ResponseTimeMs: i,
		}
		require.NoError(t, database.CreateAuditLog(log))
		logs = append(logs, log)
	}
	return logs
}

// tamper runs statements with the append-only triggers switched off, as someone with superuser access to the
// database could.
func tamper(t *testing.T, database *gwdb.Database, statements ...string) {
	t.Helper()
	tx, err := database.DB().Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec("SET LOCAL session_replication_role = replica")
	require.NoError(t, err)
	for _, statement := range statements {
		_, err = tx.Exec(statement)
		require.NoError(t, err, statement)
	}
	require.NoError(t, tx.Commit())
}

func verify(t *testing.T, database *gwdb.Database, opts auditverify.Options) *auditverify.Report {
	t.Helper()
	opts.Secret = []byte(testSecret)
	report, err := auditverify.Run(context.Background(), database, opts)
	require.NoError(t, err)
	return report
}

// requireFailure checks the report failed with a message containing every part.
func requireFailure(t *testing.T, report *auditverify.Report, parts ...string) {
	t.Helper()
	require.False(t, report.OK(), "verification should fail")
	for _, failure := range report.Failures {
		if containsAll(failure, parts) {
			return
		}
	}
	t.Fatalf("no failure mentions %q; failures: %q", parts, report.Failures)
}

func requireOK(t *testing.T, report *auditverify.Report) {
	t.Helper()
	require.True(t, report.OK(), "unexpected failures: %q", report.Failures)
}

func containsAll(message string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(message, part) {
			return false
		}
	}
	return true
}

func int64Ptr(value int64) *int64 {
	return &value
}
