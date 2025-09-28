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

type DeployRequestStage string

const (
	DeployRequestStageBuild   DeployRequestStage = "build"
	DeployRequestStageObject  DeployRequestStage = "object"
	DeployRequestStageRelease DeployRequestStage = "release"
	DeployRequestStagePromote DeployRequestStage = "promote"
)

const (
	DeployRequestStatusPending  = "pending"
	DeployRequestStatusApproved = "approved"
	DeployRequestStatusRejected = "rejected"
	DeployRequestStatusConsumed = "consumed"
)

var (
	ErrDeployRequestActive        = errors.New("a deploy request is already pending or approved for this token")
	ErrDeployRequestNotFound      = errors.New("deploy request not found")
	ErrDeployApprovalMissing      = errors.New("deployment approval required")
	ErrDeployRequestStageMismatch = errors.New("deploy approval not in required stage")
	ErrDeployRequestExpired       = errors.New("deploy approval expired")
)

type DeployRequestConflictError struct {
	Request *DeployRequest
}

func (e *DeployRequestConflictError) Error() string {
	return ErrDeployRequestActive.Error()
}

func (e *DeployRequestConflictError) Unwrap() error {
	return ErrDeployRequestActive
}

const deployRequestSelect = `
SELECT
    dr.id,
    dr.rack,
    dr.message,
    dr.status,
    dr.created_at,
    dr.updated_at,
    dr.created_by_user_id,
    created_user.email,
    created_user.name,
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
    dr.build_id,
    dr.build_created_at,
    dr.object_key,
    dr.object_created_at,
    dr.release_id,
    dr.release_created_at,
    dr.release_promoted_at,
    dr.release_promoted_by_api_token_id
FROM deploy_requests dr
LEFT JOIN users created_user ON created_user.id = dr.created_by_user_id
LEFT JOIN api_tokens target_token ON target_token.id = dr.target_api_token_id
LEFT JOIN users approved_user ON approved_user.id = dr.approved_by_user_id
LEFT JOIN users rejected_user ON rejected_user.id = dr.rejected_by_user_id
`

func scanDeployRequest(scanner rowScanner) (*DeployRequest, error) {
	var dr DeployRequest
	var (
		createdByUserID   sql.NullInt64
		createdByEmail    sql.NullString
		createdByName     sql.NullString
		targetTokenPublic sql.NullString
		targetTokenName   sql.NullString
		targetUserID      sql.NullInt64
		approvedByUserID  sql.NullInt64
		approvedByEmail   sql.NullString
		approvedByName    sql.NullString
		approvedAt        sql.NullTime
		approvalExpiresAt sql.NullTime
		rejectedByUserID  sql.NullInt64
		rejectedByEmail   sql.NullString
		rejectedByName    sql.NullString
		rejectedAt        sql.NullTime
		approvalNotes     sql.NullString
		buildID           sql.NullString
		buildCreatedAt    sql.NullTime
		objectKey         sql.NullString
		objectCreatedAt   sql.NullTime
		releaseID         sql.NullString
		releaseCreatedAt  sql.NullTime
		releasePromotedAt sql.NullTime
		releasePromotedBy sql.NullInt64
	)

	if err := scanner.Scan(
		&dr.ID,
		&dr.Rack,
		&dr.Message,
		&dr.Status,
		&dr.CreatedAt,
		&dr.UpdatedAt,
		&createdByUserID,
		&createdByEmail,
		&createdByName,
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
		&buildID,
		&buildCreatedAt,
		&objectKey,
		&objectCreatedAt,
		&releaseID,
		&releaseCreatedAt,
		&releasePromotedAt,
		&releasePromotedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeployRequestNotFound
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
	if buildID.Valid {
		dr.BuildID = buildID.String
	}
	if buildCreatedAt.Valid {
		t := buildCreatedAt.Time
		dr.BuildCreatedAt = &t
	}
	if objectKey.Valid {
		dr.ObjectKey = objectKey.String
	}
	if objectCreatedAt.Valid {
		t := objectCreatedAt.Time
		dr.ObjectCreatedAt = &t
	}
	if releaseID.Valid {
		dr.ReleaseID = releaseID.String
	}
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

func (d *Database) CreateDeployRequest(rack, message string, createdByUserID int64, createdByAPITokenID *int64, targetAPITokenID int64, targetUserID *int64) (*DeployRequest, error) {
	if strings.TrimSpace(rack) == "" {
		rack = "default"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, fmt.Errorf("message is required")
	}
	if targetAPITokenID <= 0 {
		return nil, fmt.Errorf("target api token required")
	}

	existing, err := d.activeDeployRequestByMessage(rack, message)
	if err == nil && existing != nil {
		return nil, &DeployRequestConflictError{Request: existing}
	}
	if err != nil && !errors.Is(err, ErrDeployRequestNotFound) {
		return nil, fmt.Errorf("failed to check existing deploy requests: %w", err)
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
		`INSERT INTO deploy_requests (rack, message, status, created_by_user_id, created_by_api_token_id, target_api_token_id, target_user_id)
         VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		rack,
		message,
		DeployRequestStatusPending,
		createdByUserID,
		createdAPIToken,
		targetAPITokenID,
		tgtUser,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create deploy request: %w", err)
	}
	return d.GetDeployRequest(id)
}

func (d *Database) GetDeployRequest(id int64) (*DeployRequest, error) {
	row := d.queryRow(deployRequestSelect+" WHERE dr.id = ?", id)
	return scanDeployRequest(row)
}

func (d *Database) activeDeployRequestByMessage(rack, message string) (*DeployRequest, error) {
	trimmedRack := strings.TrimSpace(rack)
	if trimmedRack == "" {
		trimmedRack = "default"
	}
	trimmedMessage := strings.TrimSpace(message)
	row := d.queryRow(
		deployRequestSelect+` WHERE dr.rack = ? AND dr.message = ? AND dr.status IN ('pending','approved') ORDER BY dr.created_at DESC LIMIT 1`,
		trimmedRack,
		trimmedMessage,
	)
	return scanDeployRequest(row)
}

func (d *Database) ActiveDeployRequestByToken(tokenID int64) (*DeployRequest, error) {
	row := d.queryRow(
		deployRequestSelect+` WHERE dr.target_api_token_id = ? AND dr.status IN ('pending','approved') ORDER BY dr.created_at DESC LIMIT 1`,
		tokenID,
	)
	return scanDeployRequest(row)
}

func (d *Database) GetDeployRequestForUser(id, userID int64) (*DeployRequest, error) {
	row := d.queryRow(deployRequestSelect+" WHERE dr.id = ? AND dr.created_by_user_id = ?", id, userID)
	return scanDeployRequest(row)
}

type DeployRequestListOptions struct {
	Status   string
	Limit    int
	Offset   int
	OnlyOpen bool
	TokenID  int64
}

func (d *Database) ListDeployRequests(opts DeployRequestListOptions) ([]*DeployRequest, error) {
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
	query := deployRequestSelect + " WHERE " + strings.Join(clauses, " AND ") + " ORDER BY dr.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, opts.Offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list deploy requests: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*DeployRequest
	for rows.Next() {
		req, err := scanDeployRequest(rows)
		if err != nil {
			if errors.Is(err, ErrDeployRequestNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

func (d *Database) ApproveDeployRequest(id int64, approverUserID int64, expiresAt time.Time, notes string) (*DeployRequest, error) {
	res, err := d.exec(
		`UPDATE deploy_requests
         SET status = ?, approved_by_user_id = ?, approved_at = NOW(), approval_expires_at = ?, approval_notes = ?, updated_at = NOW()
         WHERE id = ? AND status = ?`,
		DeployRequestStatusApproved,
		approverUserID,
		expiresAt,
		strings.TrimSpace(notes),
		id,
		DeployRequestStatusPending,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to approve deploy request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to approve deploy request: %w", err)
	}
	if rows == 0 {
		return nil, ErrDeployRequestNotFound
	}
	return d.GetDeployRequest(id)
}

func (d *Database) RejectDeployRequest(id int64, approverUserID int64, notes string) (*DeployRequest, error) {
	res, err := d.exec(
		`UPDATE deploy_requests
         SET status = ?, rejected_by_user_id = ?, rejected_at = NOW(), approval_notes = ?, updated_at = NOW()
         WHERE id = ? AND status IN (?, ?)`,
		DeployRequestStatusRejected,
		approverUserID,
		strings.TrimSpace(notes),
		id,
		DeployRequestStatusPending,
		DeployRequestStatusApproved,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to reject deploy request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to reject deploy request: %w", err)
	}
	if rows == 0 {
		return nil, ErrDeployRequestNotFound
	}
	return d.GetDeployRequest(id)
}

func (d *Database) ActiveDeployRequestForStage(tokenID int64, rack string, stage DeployRequestStage, window time.Duration) (*DeployRequest, error) {
	query := deployRequestSelect + `
WHERE dr.target_api_token_id = ?
  AND dr.status = ?
  AND dr.rack = ?
  AND dr.approved_at IS NOT NULL
LIMIT 1
`
	row := d.queryRow(query, tokenID, DeployRequestStatusApproved, rack)
	req, err := scanDeployRequest(row)
	if err != nil {
		if errors.Is(err, ErrDeployRequestNotFound) {
			return nil, ErrDeployApprovalMissing
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeployApprovalMissing
		}
		return nil, err
	}

	now := time.Now()
	if (req.ApprovalExpiresAt == nil || req.ApprovalExpiresAt.IsZero()) && req.ApprovedAt != nil && window > 0 {
		exp := req.ApprovedAt.Add(window)
		req.ApprovalExpiresAt = &exp
	}
	if req.ApprovalExpiresAt != nil && now.After(*req.ApprovalExpiresAt) {
		_, _ = d.exec("UPDATE deploy_requests SET status = ?, updated_at = NOW() WHERE id = ? AND status = ?", DeployRequestStatusConsumed, req.ID, DeployRequestStatusApproved)
		return nil, ErrDeployRequestExpired
	}

	switch stage {
	case DeployRequestStageObject:
		if strings.TrimSpace(req.BuildID) != "" || strings.TrimSpace(req.ObjectKey) != "" {
			return nil, ErrDeployRequestStageMismatch
		}
	case DeployRequestStageBuild:
		if strings.TrimSpace(req.ObjectKey) == "" || strings.TrimSpace(req.BuildID) != "" {
			return nil, ErrDeployRequestStageMismatch
		}
	case DeployRequestStageRelease:
		if strings.TrimSpace(req.BuildID) == "" || strings.TrimSpace(req.ReleaseID) != "" {
			return nil, ErrDeployRequestStageMismatch
		}
	case DeployRequestStagePromote:
		if strings.TrimSpace(req.ReleaseID) == "" || req.ReleasePromotedAt != nil {
			return nil, ErrDeployRequestStageMismatch
		}
	default:
		return nil, fmt.Errorf("unknown deploy request stage: %s", stage)
	}

	return req, nil
}

func (d *Database) MarkDeployRequestBuildUsed(id int64, buildID string, when time.Time) error {
	if strings.TrimSpace(buildID) == "" {
		return fmt.Errorf("build id required")
	}
	res, err := d.exec(
		`UPDATE deploy_requests
         SET build_id = ?, build_created_at = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND object_key IS NOT NULL AND build_id IS NULL`,
		buildID,
		when,
		id,
		DeployRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to mark build usage: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to mark build usage: %w", err)
	}
	if rows == 0 {
		return ErrDeployRequestStageMismatch
	}
	return nil
}

func (d *Database) MarkDeployRequestObjectUsed(id int64, objectKey string, when time.Time) error {
	if strings.TrimSpace(objectKey) == "" {
		return fmt.Errorf("object key required")
	}
	res, err := d.exec(
		`UPDATE deploy_requests
         SET object_key = ?, object_created_at = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND build_id IS NULL AND object_key IS NULL`,
		objectKey,
		when,
		id,
		DeployRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to mark object usage: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to mark object usage: %w", err)
	}
	if rows == 0 {
		return ErrDeployRequestStageMismatch
	}
	return nil
}

func (d *Database) MarkDeployRequestReleaseCreated(id int64, releaseID string, when time.Time) error {
	if strings.TrimSpace(releaseID) == "" {
		return fmt.Errorf("release id required")
	}
	res, err := d.exec(
		`UPDATE deploy_requests
         SET release_id = ?, release_created_at = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND build_id IS NOT NULL AND object_key IS NOT NULL AND release_id IS NULL`,
		releaseID,
		when,
		id,
		DeployRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to mark release creation: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to mark release creation: %w", err)
	}
	if rows == 0 {
		return ErrDeployRequestStageMismatch
	}
	return nil
}

func (d *Database) MarkDeployRequestPromoted(id int64, releaseID string, tokenID int64, when time.Time) error {
	if strings.TrimSpace(releaseID) == "" {
		return fmt.Errorf("release id required")
	}
	res, err := d.exec(
		`UPDATE deploy_requests
         SET release_promoted_at = ?, release_promoted_by_api_token_id = ?, status = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND release_id = ? AND release_promoted_at IS NULL`,
		when,
		tokenID,
		DeployRequestStatusConsumed,
		id,
		DeployRequestStatusApproved,
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
		return ErrDeployRequestStageMismatch
	}
	return nil
}
