package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type rowScanner interface {
	Scan(dest ...interface{}) error
}

const (
	DeployApprovalRequestStatusPending  = "pending"
	DeployApprovalRequestStatusApproved = "approved"
	DeployApprovalRequestStatusRejected = "rejected"
	DeployApprovalRequestStatusConsumed = "consumed"
)

var (
	ErrDeployApprovalRequestActive   = errors.New("a deploy approval request is already pending or approved for this token and release")
	ErrDeployApprovalRequestNotFound = errors.New("deploy approval request not found")
	ErrDeployApprovalMissing         = errors.New("deployment approval required")
	ErrDeployApprovalRequestExpired  = errors.New("deploy approval expired")
)

type DeployApprovalRequestConflictError struct {
	Request *DeployApprovalRequest
}

func (e *DeployApprovalRequestConflictError) Error() string {
	return ErrDeployApprovalRequestActive.Error()
}

func (e *DeployApprovalRequestConflictError) Unwrap() error {
	return ErrDeployApprovalRequestActive
}

const deployApprovalRequestSelect = `
SELECT
    dr.id,
    dr.message,
    dr.status,
    dr.created_at,
    dr.updated_at,
    dr.created_by_user_id,
    created_user.email,
    created_user.name,
    dr.created_by_api_token_id,
    created_token.public_id,
    created_token.name,
    dr.target_api_token_id,
    target_token.public_id,
    target_token.name,
    dr.target_user_id,
    dr.approved_by_user_id,
    approved_user.email,
    approved_user.name,
    dr.approved_at,
    dr.approval_expires_at,
    dr.rejected_by_user_id,
    rejected_user.email,
    rejected_user.name,
    dr.rejected_at,
    dr.approval_notes,
    dr.app,
    dr.release_id,
    dr.release_created_at,
    dr.release_promoted_at,
    dr.release_promoted_by_api_token_id
FROM deploy_approval_requests dr
LEFT JOIN users created_user ON created_user.id = dr.created_by_user_id
LEFT JOIN api_tokens created_token ON created_token.id = dr.created_by_api_token_id
LEFT JOIN api_tokens target_token ON target_token.id = dr.target_api_token_id
LEFT JOIN users approved_user ON approved_user.id = dr.approved_by_user_id
LEFT JOIN users rejected_user ON rejected_user.id = dr.rejected_by_user_id
`

func scanDeployApprovalRequest(scanner rowScanner) (*DeployApprovalRequest, error) {
	var dr DeployApprovalRequest
	var (
		createdByUserID    sql.NullInt64
		createdByEmail     sql.NullString
		createdByName      sql.NullString
		createdByTokenID   sql.NullInt64
		createdTokenPublic sql.NullString
		createdTokenName   sql.NullString
		targetTokenPublic  sql.NullString
		targetTokenName    sql.NullString
		targetUserID       sql.NullInt64
		approvedByUserID   sql.NullInt64
		approvedByEmail    sql.NullString
		approvedByName     sql.NullString
		approvedAt         sql.NullTime
		approvalExpiresAt  sql.NullTime
		rejectedByUserID   sql.NullInt64
		rejectedByEmail    sql.NullString
		rejectedByName     sql.NullString
		rejectedAt         sql.NullTime
		approvalNotes      sql.NullString
		app                string
		releaseID          string
		releaseCreatedAt   sql.NullTime
		releasePromotedAt  sql.NullTime
		releasePromotedBy  sql.NullInt64
	)

	if err := scanner.Scan(
		&dr.ID,
		&dr.Message,
		&dr.Status,
		&dr.CreatedAt,
		&dr.UpdatedAt,
		&createdByUserID,
		&createdByEmail,
		&createdByName,
		&createdByTokenID,
		&createdTokenPublic,
		&createdTokenName,
		&dr.TargetAPITokenID,
		&targetTokenPublic,
		&targetTokenName,
		&targetUserID,
		&approvedByUserID,
		&approvedByEmail,
		&approvedByName,
		&approvedAt,
		&approvalExpiresAt,
		&rejectedByUserID,
		&rejectedByEmail,
		&rejectedByName,
		&rejectedAt,
		&approvalNotes,
		&app,
		&releaseID,
		&releaseCreatedAt,
		&releasePromotedAt,
		&releasePromotedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeployApprovalRequestNotFound
		}
		return nil, err
	}

	if createdByUserID.Valid {
		dr.CreatedByUserID = &createdByUserID.Int64
	}
	if createdByEmail.Valid {
		dr.CreatedByEmail = createdByEmail.String
	}
	if createdByName.Valid {
		dr.CreatedByName = createdByName.String
	}
	if createdByTokenID.Valid {
		dr.CreatedByAPITokenID = &createdByTokenID.Int64
	}
	if createdTokenPublic.Valid {
		dr.CreatedByAPITokenPublicID = createdTokenPublic.String
	}
	if createdTokenName.Valid {
		dr.CreatedByAPITokenName = createdTokenName.String
	}
	if targetTokenPublic.Valid {
		dr.TargetAPITokenPublicID = targetTokenPublic.String
	}
	if targetTokenName.Valid {
		dr.TargetAPITokenName = targetTokenName.String
	}
	if targetUserID.Valid {
		dr.TargetUserID = &targetUserID.Int64
	}
	if approvedByUserID.Valid {
		dr.ApprovedByUserID = &approvedByUserID.Int64
	}
	if approvedByEmail.Valid {
		dr.ApprovedByEmail = approvedByEmail.String
	}
	if approvedByName.Valid {
		dr.ApprovedByName = approvedByName.String
	}
	if approvedAt.Valid {
		t := approvedAt.Time
		dr.ApprovedAt = &t
	}
	if approvalExpiresAt.Valid {
		t := approvalExpiresAt.Time
		dr.ApprovalExpiresAt = &t
	}
	if rejectedByUserID.Valid {
		dr.RejectedByUserID = &rejectedByUserID.Int64
	}
	if rejectedByEmail.Valid {
		dr.RejectedByEmail = rejectedByEmail.String
	}
	if rejectedByName.Valid {
		dr.RejectedByName = rejectedByName.String
	}
	if rejectedAt.Valid {
		t := rejectedAt.Time
		dr.RejectedAt = &t
	}
	if approvalNotes.Valid {
		dr.ApprovalNotes = approvalNotes.String
	}
	dr.App = app
	dr.ReleaseID = releaseID
	if releaseCreatedAt.Valid {
		t := releaseCreatedAt.Time
		dr.ReleaseCreatedAt = &t
	}
	if releasePromotedAt.Valid {
		t := releasePromotedAt.Time
		dr.ReleasePromotedAt = &t
	}
	if releasePromotedBy.Valid {
		dr.ReleasePromotedByAPITokenID = &releasePromotedBy.Int64
	}
	return &dr, nil
}

func (d *Database) CreateDeployApprovalRequest(message, app, releaseID string, createdByUserID int64, createdByAPITokenID *int64, targetAPITokenID int64, targetUserID *int64) (*DeployApprovalRequest, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, fmt.Errorf("message is required")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("app is required")
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release_id is required")
	}
	if targetAPITokenID <= 0 {
		return nil, fmt.Errorf("target api token required")
	}

	// Check for existing approval with same (app, token, release_id) triple
	existing, err := d.ActiveDeployApprovalRequestByTokenAndRelease(targetAPITokenID, app, releaseID)
	if err == nil && existing != nil {
		return nil, &DeployApprovalRequestConflictError{Request: existing}
	}
	if err != nil && !errors.Is(err, ErrDeployApprovalRequestNotFound) {
		return nil, fmt.Errorf("failed to check existing deploy approval requests: %w", err)
	}

	var createdAPIToken sql.NullInt64
	if createdByAPITokenID != nil {
		createdAPIToken = sql.NullInt64{Int64: *createdByAPITokenID, Valid: true}
	}
	var tgtUser sql.NullInt64
	if targetUserID != nil {
		tgtUser = sql.NullInt64{Int64: *targetUserID, Valid: true}
	}

	var id int64
	err = d.queryRow(
		`INSERT INTO deploy_approval_requests (message, app, release_id, status, created_by_user_id, created_by_api_token_id, target_api_token_id, target_user_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		message,
		app,
		releaseID,
		DeployApprovalRequestStatusPending,
		createdByUserID,
		createdAPIToken,
		targetAPITokenID,
		tgtUser,
	).Scan(&id)
	if err != nil {
		// Check for unique constraint violation (race condition between check and insert)
		if strings.Contains(err.Error(), "idx_deploy_approval_requests_active_release") ||
			strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			// Try to fetch the conflicting record
			existing, fetchErr := d.ActiveDeployApprovalRequestByTokenAndRelease(targetAPITokenID, app, releaseID)
			if fetchErr == nil && existing != nil {
				return nil, &DeployApprovalRequestConflictError{Request: existing}
			}
			return nil, ErrDeployApprovalRequestActive
		}
		return nil, fmt.Errorf("failed to create deploy approval request: %w", err)
	}
	return d.GetDeployApprovalRequest(id)
}

func (d *Database) GetDeployApprovalRequest(id int64) (*DeployApprovalRequest, error) {
	row := d.queryRow(deployApprovalRequestSelect+" WHERE dr.id = ?", id)
	return scanDeployApprovalRequest(row)
}

func (d *Database) ActiveDeployApprovalRequestByTokenAndRelease(tokenID int64, app, releaseID string) (*DeployApprovalRequest, error) {
	row := d.queryRow(
		deployApprovalRequestSelect+` WHERE dr.target_api_token_id = ? AND dr.app = ? AND dr.release_id = ? AND dr.status IN ('pending','approved') AND (dr.approval_expires_at IS NULL OR dr.approval_expires_at > NOW()) ORDER BY dr.created_at DESC LIMIT 1`,
		tokenID,
		app,
		releaseID,
	)
	return scanDeployApprovalRequest(row)
}

// ActiveDeployApprovalRequestByTokenAndApp returns the most recent active (pending or approved, non-expired)
// deploy approval request for the given token+app, regardless of release.
func (d *Database) ActiveDeployApprovalRequestByTokenAndApp(tokenID int64, app string) (*DeployApprovalRequest, error) {
	row := d.queryRow(
		deployApprovalRequestSelect+` WHERE dr.target_api_token_id = ? AND dr.app = ? AND dr.status IN ('pending','approved') AND (dr.approval_expires_at IS NULL OR dr.approval_expires_at > NOW()) ORDER BY dr.created_at DESC LIMIT 1`,
		tokenID,
		app,
	)
	return scanDeployApprovalRequest(row)
}

func (d *Database) GetDeployApprovalRequestForUser(id, userID int64) (*DeployApprovalRequest, error) {
	row := d.queryRow(deployApprovalRequestSelect+" WHERE dr.id = ? AND dr.created_by_user_id = ?", id, userID)
	return scanDeployApprovalRequest(row)
}

type DeployApprovalRequestListOptions struct {
	Status   string
	Limit    int
	Offset   int
	OnlyOpen bool
	TokenID  int64
}

func (d *Database) ListDeployApprovalRequests(opts DeployApprovalRequestListOptions) ([]*DeployApprovalRequest, error) {
	clauses := []string{"TRUE"}
	args := []interface{}{}
	if opts.Status != "" {
		clauses = append(clauses, "dr.status = ?")
		args = append(args, opts.Status)
	}
	if opts.OnlyOpen {
		clauses = append(clauses, "dr.status IN ('pending','approved')")
	}
	if opts.TokenID > 0 {
		clauses = append(clauses, "dr.target_api_token_id = ?")
		args = append(args, opts.TokenID)
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := deployApprovalRequestSelect + " WHERE " + strings.Join(clauses, " AND ") + " ORDER BY dr.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, opts.Offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list deploy approval requests: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*DeployApprovalRequest
	for rows.Next() {
		req, err := scanDeployApprovalRequest(rows)
		if err != nil {
			if errors.Is(err, ErrDeployApprovalRequestNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

func (d *Database) ApproveDeployApprovalRequest(id int64, approverUserID int64, expiresAt time.Time, notes string) (*DeployApprovalRequest, error) {
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET status = ?, approved_by_user_id = ?, approved_at = NOW(), approval_expires_at = ?, approval_notes = ?, updated_at = NOW()
         WHERE id = ? AND status = ?`,
		DeployApprovalRequestStatusApproved,
		approverUserID,
		expiresAt,
		strings.TrimSpace(notes),
		id,
		DeployApprovalRequestStatusPending,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to approve deploy approval request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to approve deploy approval request: %w", err)
	}
	if rows == 0 {
		return nil, ErrDeployApprovalRequestNotFound
	}
	return d.GetDeployApprovalRequest(id)
}

func (d *Database) RejectDeployApprovalRequest(id int64, approverUserID int64, notes string) (*DeployApprovalRequest, error) {
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET status = ?, rejected_by_user_id = ?, rejected_at = NOW(), approval_notes = ?, updated_at = NOW()
         WHERE id = ? AND status IN (?, ?)`,
		DeployApprovalRequestStatusRejected,
		approverUserID,
		strings.TrimSpace(notes),
		id,
		DeployApprovalRequestStatusPending,
		DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to reject deploy approval request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to reject deploy approval request: %w", err)
	}
	if rows == 0 {
		return nil, ErrDeployApprovalRequestNotFound
	}
	return d.GetDeployApprovalRequest(id)
}

func (d *Database) MarkDeployApprovalRequestPromoted(id int64, app, releaseID string, tokenID int64, when time.Time) error {
	if strings.TrimSpace(app) == "" {
		return fmt.Errorf("app is required")
	}
	if strings.TrimSpace(releaseID) == "" {
		return fmt.Errorf("release id required")
	}
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET release_promoted_at = ?, release_promoted_by_api_token_id = ?, status = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND app = ? AND release_id = ? AND release_promoted_at IS NULL`,
		when,
		tokenID,
		DeployApprovalRequestStatusConsumed,
		id,
		DeployApprovalRequestStatusApproved,
		app,
		releaseID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark release promotion: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to mark release promotion: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deployment approval not found or already promoted")
	}
	return nil
}
