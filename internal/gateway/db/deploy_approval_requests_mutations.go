package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreateDeployApprovalRequest creates a new deploy approval request for the specified parameters.
func (d *Database) CreateDeployApprovalRequest(
	message, app, gitCommitHash, gitBranch, prURL string,
	ciMetadata []byte,
	createdByUserID int64,
	createdByAPITokenID *int64,
	targetAPITokenID int64,
) (*DeployApprovalRequest, error) {
	normalizedMessage, normalizedApp, normalizedGitCommitHash, err := validateAndNormalizeDeployApprovalInput(
		message, app, gitCommitHash, targetAPITokenID,
	)
	if err != nil {
		return nil, err
	}

	if err := d.checkDeployApprovalConflict(targetAPITokenID, normalizedGitCommitHash); err != nil {
		return nil, err
	}

	nullValues := buildDeployApprovalNullValues(createdByAPITokenID, gitBranch, prURL)

	id, err := d.insertDeployApprovalRequest(
		normalizedMessage, normalizedApp, normalizedGitCommitHash, ciMetadata,
		createdByUserID, nullValues, targetAPITokenID,
	)
	if err != nil {
		return d.handleDeployApprovalInsertError(err, targetAPITokenID, normalizedGitCommitHash)
	}
	return d.GetDeployApprovalRequest(id)
}

func validateAndNormalizeDeployApprovalInput(
	message, app, gitCommitHash string,
	targetAPITokenID int64,
) (string, string, string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", "", "", fmt.Errorf("message is required")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return "", "", "", fmt.Errorf("app is required")
	}
	gitCommitHash = strings.TrimSpace(gitCommitHash)
	if gitCommitHash == "" {
		return "", "", "", fmt.Errorf("git_commit_hash is required")
	}
	if targetAPITokenID <= 0 {
		return "", "", "", fmt.Errorf("target api token required")
	}
	return message, app, gitCommitHash, nil
}

func (d *Database) checkDeployApprovalConflict(tokenID int64, gitCommitHash string) error {
	// Check for unexpired approved requests
	existing, err := d.FindDeployApprovalRequest(DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: gitCommitHash,
		StatusFilter:  DeployApprovalRequestStatusApproved, // FindDeployApprovalRequest enforces expiry for "approved"
	})
	if err != nil && !errors.Is(err, ErrDeployApprovalRequestNotFound) {
		return fmt.Errorf("failed to check existing approved requests: %w", err)
	}

	if existing == nil {
		// Check for pending requests
		existing, err = d.FindDeployApprovalRequest(DeployApprovalLookup{
			TokenID:       tokenID,
			GitCommitHash: gitCommitHash,
			StatusFilter:  DeployApprovalRequestStatusPending,
		})
		if err != nil && !errors.Is(err, ErrDeployApprovalRequestNotFound) {
			return fmt.Errorf("failed to check existing pending requests: %w", err)
		}
	}

	if existing != nil {
		return &DeployApprovalRequestConflictError{Request: existing}
	}
	return nil
}

type deployApprovalNullValues struct {
	createdAPIToken sql.NullInt64
	gitBranch       sql.NullString
	prURL           sql.NullString
}

func buildDeployApprovalNullValues(
	createdByAPITokenID *int64,
	gitBranch, prURL string,
) deployApprovalNullValues {
	var result deployApprovalNullValues
	if createdByAPITokenID != nil {
		result.createdAPIToken = sql.NullInt64{Int64: *createdByAPITokenID, Valid: true}
	}
	if trimmed := strings.TrimSpace(gitBranch); trimmed != "" {
		result.gitBranch = sql.NullString{String: trimmed, Valid: true}
	}
	if trimmed := strings.TrimSpace(prURL); trimmed != "" {
		result.prURL = sql.NullString{String: trimmed, Valid: true}
	}
	return result
}

const deployApprovalInsertSQL = `INSERT INTO deploy_approval_requests
	(message, app, git_commit_hash, git_branch, pr_url, ci_metadata, status,
	created_by_user_id, created_by_api_token_id, target_api_token_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`

func (d *Database) insertDeployApprovalRequest(
	message, app, gitCommitHash string,
	ciMetadata []byte,
	createdByUserID int64,
	nulls deployApprovalNullValues,
	targetAPITokenID int64,
) (int64, error) {
	var id int64
	err := d.queryRow(
		deployApprovalInsertSQL,
		message,
		app,
		gitCommitHash,
		nulls.gitBranch,
		nulls.prURL,
		ciMetadata,
		DeployApprovalRequestStatusPending,
		createdByUserID,
		nulls.createdAPIToken,
		targetAPITokenID,
	).Scan(&id)
	return id, err
}

func (d *Database) handleDeployApprovalInsertError(
	err error,
	tokenID int64,
	normalizedGitCommitHash string,
) (*DeployApprovalRequest, error) {
	if !isUniqueConstraintViolation(err) {
		return nil, fmt.Errorf("failed to create deploy approval request: %w", err)
	}

	existing, fetchErr := d.FindDeployApprovalRequest(DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: normalizedGitCommitHash,
		StatusFilter:  "any",
	})
	if fetchErr == nil && existing != nil {
		return nil, &DeployApprovalRequestConflictError{Request: existing}
	}
	return nil, ErrDeployApprovalRequestActive
}

func isUniqueConstraintViolation(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "idx_deploy_approval_requests_active_commit") ||
		strings.Contains(errMsg, "duplicate key value violates unique constraint")
}

// updateApprovalStatus updates the status of a deploy approval request.
// whereClause should be "id = ?" or "public_id = ?".
// whereArg is the corresponding ID or publicID value.
func (d *Database) updateApprovalStatus(
	whereClause string,
	whereArg interface{},
	newStatus string,
	actorUserID int64,
	approvalExpiresAt *time.Time,
	notes string,
	allowedStatuses []string,
) error {
	trimmedNotes := strings.TrimSpace(notes)

	var whereColumn string
	switch whereClause {
	case "id = ?":
		whereColumn = "id"
	case "public_id = ?":
		whereColumn = "public_id"
	default:
		return fmt.Errorf("invalid where clause: %s", whereClause)
	}

	var updateSQL string
	var args []interface{}

	switch newStatus {
	case DeployApprovalRequestStatusApproved:
		if len(allowedStatuses) < 1 {
			return fmt.Errorf("allowedStatuses must include at least one allowed status")
		}
		if approvalExpiresAt == nil {
			return fmt.Errorf("approval expiration timestamp required for approved status")
		}
		updateSQL = `UPDATE deploy_approval_requests
         SET status = ?, approved_by_user_id = ?, approved_at = NOW(),
             approval_expires_at = ?, approval_notes = ?, updated_at = NOW()
         WHERE ` + whereColumn + ` = ? AND status = ?`
		args = []interface{}{newStatus, actorUserID, *approvalExpiresAt, trimmedNotes, whereArg, allowedStatuses[0]}
	case DeployApprovalRequestStatusRejected:
		if len(allowedStatuses) < 2 {
			return fmt.Errorf("allowedStatuses must include two allowed statuses for rejection")
		}
		updateSQL = `UPDATE deploy_approval_requests
         SET status = ?, rejected_by_user_id = ?, rejected_at = NOW(), approval_notes = ?, updated_at = NOW()
         WHERE ` + whereColumn + ` = ? AND status IN (?, ?)`
		args = []interface{}{newStatus, actorUserID, trimmedNotes, whereArg, allowedStatuses[0], allowedStatuses[1]}
	default:
		return fmt.Errorf("unsupported status: %s", newStatus)
	}

	res, err := d.exec(updateSQL, args...)
	if err != nil {
		return fmt.Errorf("failed to update deploy approval request status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to update deploy approval request status: %w", err)
	}
	if rows == 0 {
		return ErrDeployApprovalRequestNotFound
	}
	return nil
}

// ApproveDeployApprovalRequest approves a pending deploy approval request by internal ID
func (d *Database) ApproveDeployApprovalRequest(
	id int64,
	approverUserID int64,
	expiresAt time.Time,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"id = ?",
		id,
		DeployApprovalRequestStatusApproved,
		approverUserID,
		&expiresAt,
		notes,
		[]string{DeployApprovalRequestStatusPending},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequest(id)
}

// ApproveDeployApprovalRequestByPublicID approves a pending deploy approval request by public ID
func (d *Database) ApproveDeployApprovalRequestByPublicID(
	publicID string,
	approverUserID int64,
	expiresAt time.Time,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"public_id = ?",
		publicID,
		DeployApprovalRequestStatusApproved,
		approverUserID,
		&expiresAt,
		notes,
		[]string{DeployApprovalRequestStatusPending},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequestByPublicID(publicID)
}

// RejectDeployApprovalRequest rejects a pending or approved deploy approval request by internal ID
func (d *Database) RejectDeployApprovalRequest(
	id int64,
	approverUserID int64,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"id = ?",
		id,
		DeployApprovalRequestStatusRejected,
		approverUserID,
		nil,
		notes,
		[]string{DeployApprovalRequestStatusPending, DeployApprovalRequestStatusApproved},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequest(id)
}

// RejectDeployApprovalRequestByPublicID rejects a pending or approved deploy approval request by public ID
func (d *Database) RejectDeployApprovalRequestByPublicID(
	publicID string,
	approverUserID int64,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"public_id = ?",
		publicID,
		DeployApprovalRequestStatusRejected,
		approverUserID,
		nil,
		notes,
		[]string{DeployApprovalRequestStatusPending, DeployApprovalRequestStatusApproved},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequestByPublicID(publicID)
}

// UpdateDeployApprovalRequestObjectURL updates the object URL for an approved deploy approval request
func (d *Database) UpdateDeployApprovalRequestObjectURL(id int64, objectURL string) error {
	if strings.TrimSpace(objectURL) == "" {
		return fmt.Errorf("object url required")
	}
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET object_url = ?, updated_at = NOW()
         WHERE id = ? AND status = ?`,
		objectURL,
		id,
		DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to update object url tracking: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to update object url tracking: %w", err)
	}
	if rows == 0 {
		return ErrDeployApprovalRequestNotFound
	}
	return nil
}

// ExtendDeployApprovalRequestExpiry extends the expiry time for an approved deploy approval request
func (d *Database) ExtendDeployApprovalRequestExpiry(
	publicID string,
	newExpiresAt time.Time,
) (*DeployApprovalRequest, error) {
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET approval_expires_at = ?, updated_at = NOW()
         WHERE public_id = ? AND status = ? AND (approval_expires_at IS NULL OR approval_expires_at < ?)`,
		newExpiresAt,
		publicID,
		DeployApprovalRequestStatusApproved,
		newExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extend deploy approval expiry: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to extend deploy approval expiry: %w", err)
	}
	if rows == 0 {
		return nil, ErrDeployApprovalRequestNotFound
	}
	return d.GetDeployApprovalRequestByPublicID(publicID)
}
