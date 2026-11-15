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

	if err := d.CheckDeployApprovalConflict(targetAPITokenID, normalizedGitCommitHash, normalizedApp); err != nil {
		return nil, err
	}

	nullValues := buildDeployApprovalNullValues(createdByAPITokenID, gitBranch, prURL)

	id, err := d.insertDeployApprovalRequest(
		normalizedMessage, normalizedApp, normalizedGitCommitHash, ciMetadata,
		createdByUserID, nullValues, targetAPITokenID,
	)
	if err != nil {
		return d.handleDeployApprovalInsertError(err, targetAPITokenID, normalizedGitCommitHash, normalizedApp)
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
	gitCommitHash, ok := NormalizeCommitSHA(gitCommitHash)
	if !ok {
		return "", "", "", fmt.Errorf("git_commit_hash must be a full 40-character commit SHA")
	}
	if targetAPITokenID <= 0 {
		return "", "", "", fmt.Errorf("target api token required")
	}
	return message, app, gitCommitHash, nil
}

// CheckDeployApprovalConflict returns a conflict error carrying the open (pending or unexpired approved)
// request for this commit, target token and app, if there is one. gitCommitHash and app are normalized
// (NormalizeCommitSHA, trimmed).
func (d *Database) CheckDeployApprovalConflict(tokenID int64, gitCommitHash, app string) error {
	// Check for unexpired approved requests
	existing, err := d.FindDeployApprovalRequest(DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: gitCommitHash,
		App:           app,
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
			App:           app,
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
	normalizedGitCommitHash, app string,
) (*DeployApprovalRequest, error) {
	if !isUniqueViolation(err) {
		return nil, fmt.Errorf("failed to create deploy approval request: %w", err)
	}

	existing, fetchErr := d.FindDeployApprovalRequest(DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: normalizedGitCommitHash,
		App:           app,
		StatusFilter:  "any",
	})
	if fetchErr == nil && existing != nil {
		return nil, &DeployApprovalRequestConflictError{Request: existing}
	}
	return nil, ErrDeployApprovalRequestActive
}

// approvalStatusUpdate is an approval or a rejection of a deploy approval request.
type approvalStatusUpdate struct {
	newStatus   string
	actorUserID int64
	// expiresAt is when an approval's window ends (approvals only).
	expiresAt *time.Time
	notes     string
	// pendingMaxAge is DEPLOY_APPROVAL_PENDING_MAX_AGE (approvals only, 0 for none): a request pending longer has
	// expired, even before the expiry sweep marks it.
	pendingMaxAge time.Duration
}

// updateApprovalStatus approves a pending request, or rejects a pending or approved one whose promote hasn't
// started. whereColumn is "id" or "public_id", and whereArg its value. When no row changes, it says why:
// ErrDeployApprovalRequestTooOld, ErrDeployApprovalPromoteStarted or ErrDeployApprovalRequestNotFound.
func (d *Database) updateApprovalStatus(whereColumn string, whereArg interface{}, update approvalStatusUpdate) error {
	if whereColumn == "public_id" {
		if publicID, ok := whereArg.(string); !ok || !isPublicID(publicID) {
			return ErrDeployApprovalRequestNotFound
		}
	} else if whereColumn != "id" {
		return fmt.Errorf("invalid where column: %s", whereColumn)
	}
	notes := strings.TrimSpace(update.notes)

	var updateSQL string
	var args []interface{}
	switch update.newStatus {
	case DeployApprovalRequestStatusApproved:
		if update.expiresAt == nil {
			return fmt.Errorf("approval expiration timestamp required for approved status")
		}
		maxAgeSeconds := int64(update.pendingMaxAge / time.Second)
		updateSQL = `UPDATE deploy_approval_requests
         SET status = ?, approved_by_user_id = ?, approved_at = NOW(),
             approval_expires_at = ?, approval_notes = ?, updated_at = NOW()
         WHERE ` + whereColumn + ` = ? AND status = ?
           AND (?::bigint = 0 OR created_at > NOW() - make_interval(secs => ?::bigint))`
		args = []interface{}{
			update.newStatus, update.actorUserID, *update.expiresAt, notes, whereArg,
			DeployApprovalRequestStatusPending, maxAgeSeconds, maxAgeSeconds,
		}
	case DeployApprovalRequestStatusRejected:
		// Once a promote has been claimed the rack may be applying it, and its outcome must be recorded against an
		// approval that still says approved.
		updateSQL = `UPDATE deploy_approval_requests
         SET status = ?, rejected_by_user_id = ?, rejected_at = NOW(), approval_notes = ?, updated_at = NOW()
         WHERE ` + whereColumn + ` = ? AND status IN (?, ?) AND promote_started_at IS NULL`
		args = []interface{}{
			update.newStatus, update.actorUserID, notes, whereArg,
			DeployApprovalRequestStatusPending, DeployApprovalRequestStatusApproved,
		}
	default:
		return fmt.Errorf("unsupported status: %s", update.newStatus)
	}

	updated, err := updatedOneRow(d.exec(updateSQL, args...))
	if err != nil {
		return fmt.Errorf("failed to update deploy approval request status: %w", err)
	}
	if updated {
		return nil
	}
	return d.approvalStatusUnchangedReason(whereColumn, whereArg, update)
}

// approvalStatusUnchangedReason says why updateApprovalStatus changed nothing.
func (d *Database) approvalStatusUnchangedReason(
	whereColumn string,
	whereArg interface{},
	update approvalStatusUpdate,
) error {
	var status string
	var promoteStarted bool
	var ageSeconds float64
	err := d.queryRow(`SELECT status, promote_started_at IS NOT NULL, EXTRACT(EPOCH FROM NOW() - created_at)
		FROM deploy_approval_requests WHERE `+whereColumn+` = ?`, whereArg).Scan(&status, &promoteStarted, &ageSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeployApprovalRequestNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to read deploy approval request status: %w", err)
	}
	age := time.Duration(ageSeconds * float64(time.Second))
	switch {
	case update.newStatus == DeployApprovalRequestStatusApproved && status == DeployApprovalRequestStatusPending &&
		update.pendingMaxAge > 0 && age >= update.pendingMaxAge:
		return ErrDeployApprovalRequestTooOld
	case update.newStatus == DeployApprovalRequestStatusRejected && promoteStarted &&
		status == DeployApprovalRequestStatusApproved:
		return ErrDeployApprovalPromoteStarted
	default:
		return ErrDeployApprovalRequestNotFound
	}
}

// ApproveDeployApprovalRequest approves a pending deploy approval request by internal ID. pendingMaxAge is
// DEPLOY_APPROVAL_PENDING_MAX_AGE (0 for none).
func (d *Database) ApproveDeployApprovalRequest(
	id int64,
	approverUserID int64,
	expiresAt time.Time,
	notes string,
	pendingMaxAge time.Duration,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus("id", id, approvalStatusUpdate{
		newStatus: DeployApprovalRequestStatusApproved, actorUserID: approverUserID, expiresAt: &expiresAt,
		notes: notes, pendingMaxAge: pendingMaxAge,
	})
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequest(id)
}

// ApproveDeployApprovalRequestByPublicID approves a pending deploy approval request by public ID. pendingMaxAge
// is DEPLOY_APPROVAL_PENDING_MAX_AGE (0 for none).
func (d *Database) ApproveDeployApprovalRequestByPublicID(
	publicID string,
	approverUserID int64,
	expiresAt time.Time,
	notes string,
	pendingMaxAge time.Duration,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus("public_id", publicID, approvalStatusUpdate{
		newStatus: DeployApprovalRequestStatusApproved, actorUserID: approverUserID, expiresAt: &expiresAt,
		notes: notes, pendingMaxAge: pendingMaxAge,
	})
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
	err := d.updateApprovalStatus("id", id, approvalStatusUpdate{
		newStatus: DeployApprovalRequestStatusRejected, actorUserID: approverUserID, notes: notes,
	})
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
	err := d.updateApprovalStatus("public_id", publicID, approvalStatusUpdate{
		newStatus: DeployApprovalRequestStatusRejected, actorUserID: approverUserID, notes: notes,
	})
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequestByPublicID(publicID)
}

// ExtendDeployApprovalRequestExpiry extends the window of an approved deploy approval request. An approval whose
// window already lapsed (and was marked expired) is approved again, unless another request for the same commit,
// token and app has opened since (ErrDeployApprovalRequestActive).
func (d *Database) ExtendDeployApprovalRequestExpiry(
	publicID string,
	newExpiresAt time.Time,
) (*DeployApprovalRequest, error) {
	if !isPublicID(publicID) {
		return nil, ErrDeployApprovalRequestNotFound
	}
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET approval_expires_at = ?, status = ?, updated_at = NOW()
         WHERE public_id = ? AND status IN (?, ?) AND approved_at IS NOT NULL
           AND (approval_expires_at IS NULL OR approval_expires_at < ?)`,
		newExpiresAt,
		DeployApprovalRequestStatusApproved,
		publicID,
		DeployApprovalRequestStatusApproved,
		DeployApprovalRequestStatusExpired,
		newExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDeployApprovalRequestActive
		}
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
