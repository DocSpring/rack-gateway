package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Results of a scheduled audit verification run.
const (
	// AuditVerificationPassed: no integrity failures.
	AuditVerificationPassed = "passed"
	// AuditVerificationFailed: integrity failures.
	AuditVerificationFailed = "failed"
	// AuditVerificationError: the run couldn't read what it needed to check.
	AuditVerificationError = "error"
)

// AuditVerificationStatus is the outcome of the latest scheduled audit verification, as the Audit Logs page shows
// it. Verification doesn't rely on it: anyone who can write to the database can change it.
type AuditVerificationStatus struct {
	// CheckedAt is when the latest run started.
	CheckedAt time.Time `json:"checked_at" validate:"required"`
	// Result is AuditVerificationPassed, AuditVerificationFailed or AuditVerificationError.
	Result       string `json:"result"        validate:"required" enums:"passed,failed,error"`
	FailureCount int    `json:"failure_count" validate:"required"`
	WarningCount int    `json:"warning_count" validate:"required"`
	// Summary is the latest run's report, as verify-audit prints it, or why it couldn't run.
	Summary string `json:"summary" validate:"required"`
	// FullCheckedAt is when the last passing run that checked every event started.
	FullCheckedAt *time.Time `json:"full_checked_at,omitempty"`
	LastPassedAt  *time.Time `json:"last_passed_at,omitempty"`
	// FailingSince is when the first failing run since the last passing one started (nil while passing).
	FailingSince *time.Time `json:"failing_since,omitempty"`
}

// AuditVerificationStatus returns the latest scheduled audit verification's outcome, or nil before the first run.
func (d *Database) AuditVerificationStatus(ctx context.Context) (*AuditVerificationStatus, error) {
	status := new(AuditVerificationStatus)
	err := d.db.QueryRowContext(ctx, `
		SELECT checked_at, result, failure_count, warning_count, summary, full_checked_at, last_passed_at,
			failing_since
		FROM audit_verification_status`).Scan(
		&status.CheckedAt, &status.Result, &status.FailureCount, &status.WarningCount, &status.Summary,
		&status.FullCheckedAt, &status.LastPassedAt, &status.FailingSince,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the audit verification status: %w", err)
	}
	return status, nil
}

// SaveAuditVerificationStatus records a run's outcome, replacing the previous one.
func (d *Database) SaveAuditVerificationStatus(ctx context.Context, status *AuditVerificationStatus) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO audit_verification_status (id, checked_at, result, failure_count, warning_count, summary,
			full_checked_at, last_passed_at, failing_since)
		VALUES (TRUE, $1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			checked_at = EXCLUDED.checked_at,
			result = EXCLUDED.result,
			failure_count = EXCLUDED.failure_count,
			warning_count = EXCLUDED.warning_count,
			summary = EXCLUDED.summary,
			full_checked_at = EXCLUDED.full_checked_at,
			last_passed_at = EXCLUDED.last_passed_at,
			failing_since = EXCLUDED.failing_since`,
		status.CheckedAt, status.Result, status.FailureCount, status.WarningCount, status.Summary,
		status.FullCheckedAt, status.LastPassedAt, status.FailingSince,
	)
	if err != nil {
		return fmt.Errorf("failed to record the audit verification status: %w", err)
	}
	return nil
}
