package db

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

const deployApprovalRequestSelect = `
SELECT
    dr.id,
    dr.public_id,
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
    dr.git_commit_hash,
    dr.git_branch,
    dr.pr_url,
    dr.ci_metadata,
    dr.app,
    dr.object_url,
    dr.build_id,
    dr.release_id,
    dr.process_ids,
    dr.exec_commands
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
		gitBranch          sql.NullString
		prURL              sql.NullString
		ciMetadata         []byte
		app                sql.NullString
		objectURL          sql.NullString
		buildID            sql.NullString
		releaseID          sql.NullString
		processIDs         pq.StringArray
		execCommands       []byte // JSONB from PostgreSQL
	)

	if err := scanner.Scan(
		&dr.ID,
		&dr.PublicID,
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
		&dr.GitCommitHash,
		&gitBranch,
		&prURL,
		&ciMetadata,
		&app,
		&objectURL,
		&buildID,
		&releaseID,
		pq.Array(&processIDs),
		&execCommands,
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
	if gitBranch.Valid {
		dr.GitBranch = gitBranch.String
	}
	if prURL.Valid {
		dr.PrURL = prURL.String
	}
	if len(ciMetadata) > 0 {
		dr.CIMetadata = ciMetadata
	}
	if app.Valid {
		dr.App = app.String
	}
	if objectURL.Valid {
		dr.ObjectURL = objectURL.String
	}
	if buildID.Valid {
		dr.BuildID = buildID.String
	}
	if releaseID.Valid {
		dr.ReleaseID = releaseID.String
	}
	if len(processIDs) > 0 {
		dr.ProcessIDs = []string(processIDs)
	}
	if len(execCommands) > 0 {
		dr.ExecCommands = execCommands
	}
	return &dr, nil
}
