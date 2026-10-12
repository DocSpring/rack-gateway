package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestAuditVerificationStatusIsOneRow(t *testing.T) {
	database := dbtest.NewDatabase(t)
	ctx := context.Background()

	status, err := database.AuditVerificationStatus(ctx)
	require.NoError(t, err)
	assert.Nil(t, status, "no status before the first run")

	checked := time.Date(2026, 5, 1, 0, 30, 0, 0, time.UTC)
	require.NoError(t, database.SaveAuditVerificationStatus(ctx, &gwdb.AuditVerificationStatus{
		CheckedAt:     checked,
		Result:        gwdb.AuditVerificationPassed,
		WarningCount:  2,
		Summary:       "Result: OK\n",
		FullCheckedAt: &checked,
		LastPassedAt:  &checked,
	}))
	status, err = database.AuditVerificationStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.WithinDuration(t, checked, status.CheckedAt, 0)
	assert.Equal(t, gwdb.AuditVerificationPassed, status.Result)
	assert.Equal(t, 2, status.WarningCount)
	assert.Equal(t, "Result: OK\n", status.Summary)
	require.NotNil(t, status.FullCheckedAt)
	assert.WithinDuration(t, checked, *status.FullCheckedAt, 0)
	assert.Nil(t, status.FailingSince)

	later := checked.Add(time.Hour)
	require.NoError(t, database.SaveAuditVerificationStatus(ctx, &gwdb.AuditVerificationStatus{
		CheckedAt:    later,
		Result:       gwdb.AuditVerificationFailed,
		FailureCount: 3,
		Summary:      "Result: FAILED (3 integrity failures)\n",
		LastPassedAt: &checked,
		FailingSince: &later,
	}))
	status, err = database.AuditVerificationStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, gwdb.AuditVerificationFailed, status.Result)
	assert.Equal(t, 3, status.FailureCount)
	assert.Nil(t, status.FullCheckedAt, "the new status replaces the old one, NULLs included")
	require.NotNil(t, status.FailingSince)
	assert.WithinDuration(t, later, *status.FailingSince, 0)

	var rows int
	require.NoError(t, database.DB().QueryRow(`SELECT COUNT(*) FROM audit_verification_status`).Scan(&rows))
	assert.Equal(t, 1, rows)

	_, err = database.DB().Exec(`INSERT INTO audit_verification_status (id, checked_at, result, summary)
		VALUES (FALSE, NOW(), 'passed', '')`)
	require.Error(t, err, "there is only ever one status")
}
