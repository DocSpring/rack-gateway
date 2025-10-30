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

type deployApprovalNullables struct {
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
	processIDs         []string
	execCommands       []byte
}

func scanDeployApprovalRequest(scanner rowScanner) (*DeployApprovalRequest, error) {
	var dr DeployApprovalRequest
	var n deployApprovalNullables

	if err := scanner.Scan(
		&dr.ID, &dr.PublicID, &dr.Message, &dr.Status, &dr.CreatedAt, &dr.UpdatedAt,
		&n.createdByUserID, &n.createdByEmail, &n.createdByName,
		&n.createdByTokenID, &n.createdTokenPublic, &n.createdTokenName,
		&dr.TargetAPITokenID, &n.targetTokenPublic, &n.targetTokenName,
		&n.approvedByUserID, &n.approvedByEmail, &n.approvedByName,
		&n.approvedAt, &n.approvalExpiresAt,
		&n.rejectedByUserID, &n.rejectedByEmail, &n.rejectedByName, &n.rejectedAt,
		&n.approvalNotes, &dr.GitCommitHash, &n.gitBranch, &n.prURL,
		&n.ciMetadata, &n.app, &n.objectURL, &n.buildID, &n.releaseID,
		pq.Array(&n.processIDs), &n.execCommands,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeployApprovalRequestNotFound
		}
		return nil, err
	}

	applyDeployApprovalNullables(&dr, &n)
	return &dr, nil
}

func applyDeployApprovalNullables(dr *DeployApprovalRequest, n *deployApprovalNullables) {
	applyCreatorFields(dr, n)
	applyApprovalFields(dr, n)
	applyRejectionFields(dr, n)
	applyMetadataFields(dr, n)
}

func applyCreatorFields(dr *DeployApprovalRequest, n *deployApprovalNullables) {
	if n.createdByUserID.Valid {
		dr.CreatedByUserID = &n.createdByUserID.Int64
	}
	if n.createdByEmail.Valid {
		dr.CreatedByEmail = n.createdByEmail.String
	}
	if n.createdByName.Valid {
		dr.CreatedByName = n.createdByName.String
	}
	if n.createdByTokenID.Valid {
		dr.CreatedByAPITokenID = &n.createdByTokenID.Int64
	}
	if n.createdTokenPublic.Valid {
		dr.CreatedByAPITokenPublicID = n.createdTokenPublic.String
	}
	if n.createdTokenName.Valid {
		dr.CreatedByAPITokenName = n.createdTokenName.String
	}
	if n.targetTokenPublic.Valid {
		dr.TargetAPITokenPublicID = n.targetTokenPublic.String
	}
	if n.targetTokenName.Valid {
		dr.TargetAPITokenName = n.targetTokenName.String
	}
}

func applyApprovalFields(dr *DeployApprovalRequest, n *deployApprovalNullables) {
	if n.approvedByUserID.Valid {
		dr.ApprovedByUserID = &n.approvedByUserID.Int64
	}
	if n.approvedByEmail.Valid {
		dr.ApprovedByEmail = n.approvedByEmail.String
	}
	if n.approvedByName.Valid {
		dr.ApprovedByName = n.approvedByName.String
	}
	if n.approvedAt.Valid {
		t := n.approvedAt.Time
		dr.ApprovedAt = &t
	}
	if n.approvalExpiresAt.Valid {
		t := n.approvalExpiresAt.Time
		dr.ApprovalExpiresAt = &t
	}
	if n.approvalNotes.Valid {
		dr.ApprovalNotes = n.approvalNotes.String
	}
}

func applyRejectionFields(dr *DeployApprovalRequest, n *deployApprovalNullables) {
	if n.rejectedByUserID.Valid {
		dr.RejectedByUserID = &n.rejectedByUserID.Int64
	}
	if n.rejectedByEmail.Valid {
		dr.RejectedByEmail = n.rejectedByEmail.String
	}
	if n.rejectedByName.Valid {
		dr.RejectedByName = n.rejectedByName.String
	}
	if n.rejectedAt.Valid {
		t := n.rejectedAt.Time
		dr.RejectedAt = &t
	}
}

func applyMetadataFields(dr *DeployApprovalRequest, n *deployApprovalNullables) {
	if n.gitBranch.Valid {
		dr.GitBranch = n.gitBranch.String
	}
	if n.prURL.Valid {
		dr.PrURL = n.prURL.String
	}
	if len(n.ciMetadata) > 0 {
		dr.CIMetadata = n.ciMetadata
	}
	if n.app.Valid {
		dr.App = n.app.String
	}
	if n.objectURL.Valid {
		dr.ObjectURL = n.objectURL.String
	}
	if n.buildID.Valid {
		dr.BuildID = n.buildID.String
	}
	if n.releaseID.Valid {
		dr.ReleaseID = n.releaseID.String
	}
	if len(n.processIDs) > 0 {
		dr.ProcessIDs = append([]string{}, n.processIDs...)
	}
	if len(n.execCommands) > 0 {
		dr.ExecCommands = n.execCommands
	}
}
