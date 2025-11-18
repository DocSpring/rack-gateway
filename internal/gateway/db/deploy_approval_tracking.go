package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MarkDeployApprovalRequestBuildStarted records that a build has started for an approved deploy approval request.
// The releaseID may be empty initially since the real Convox API returns builds with an empty release field
// that gets populated later when the build completes.
func (d *Database) MarkDeployApprovalRequestBuildStarted(id int64, buildID, releaseID string) error {
	if strings.TrimSpace(buildID) == "" {
		return fmt.Errorf("build id required")
	}

	var res sql.Result
	var err error

	// Flow: build creation returns buildID (releaseID empty), then later build completion returns releaseID
	if releaseID != "" {
		// Update only release_id (build_id already set from build creation)
		res, err = d.exec(
			`UPDATE deploy_approval_requests
             SET release_id = ?,
                 updated_at = NOW()
             WHERE id = ? AND status = ?`,
			releaseID,
			id,
			DeployApprovalRequestStatusApproved,
		)
	} else {
		// Update only build_id (first call from build creation)
		res, err = d.exec(
			`UPDATE deploy_approval_requests
             SET build_id = ?,
                 updated_at = NOW()
             WHERE id = ? AND status = ?`,
			buildID,
			id,
			DeployApprovalRequestStatusApproved,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to update build tracking: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to update build tracking: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deployment approval not found or not approved")
	}
	return nil
}

// MarkDeployApprovalRequestPromoted records that a release was promoted for an approved deploy approval request
func (d *Database) MarkDeployApprovalRequestPromoted(
	id int64,
	app, releaseID string,
	tokenID int64,
	when time.Time,
) error {
	if strings.TrimSpace(app) == "" {
		return fmt.Errorf("app is required")
	}
	if strings.TrimSpace(releaseID) == "" {
		return fmt.Errorf("release id required")
	}
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET app = ?, release_id = ?, release_promoted_at = ?, release_promoted_by_api_token_id = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND release_promoted_at IS NULL`,
		app,
		releaseID,
		when,
		tokenID,
		id,
		DeployApprovalRequestStatusApproved,
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

// AppendProcessIDToDeployApprovalRequest adds a process ID to the list of processes for a deploy approval request
func (d *Database) AppendProcessIDToDeployApprovalRequest(id int64, processID string) error {
	if strings.TrimSpace(processID) == "" {
		return fmt.Errorf("process id required")
	}

	// Use PostgreSQL array_append to add process ID
	// exec_commands will be populated when actual exec commands are run
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET process_ids = array_append(COALESCE(process_ids, ARRAY[]::TEXT[]), ?),
             updated_at = NOW()
         WHERE id = ? AND status = ?`,
		processID,
		id,
		DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to append process ID to deploy approval request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to append process ID to deploy approval request: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deployment approval not found or not approved")
	}
	return nil
}

// AppendExecCommandToDeployApprovalRequest records an exec command executed as part of a deploy approval request
func (d *Database) AppendExecCommandToDeployApprovalRequest(id int64, processID, command string) error {
	if strings.TrimSpace(processID) == "" {
		return fmt.Errorf("process id required")
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command required")
	}

	// Add command to exec_commands JSONB object
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET exec_commands = COALESCE(exec_commands, '{}'::jsonb) || jsonb_build_object(?::text, ?::text),
             updated_at = NOW()
         WHERE id = ? AND status = ?`,
		processID,
		command,
		id,
		DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to append exec command to deploy approval request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to append exec command to deploy approval request: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deployment approval not found or not approved")
	}
	return nil
}

// MarkDeployApprovalAsDeployed marks a deploy approval request as deployed after successful promotion
func (d *Database) MarkDeployApprovalAsDeployed(id int64) error {
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET status = ?, updated_at = NOW()
         WHERE id = ? AND status = ?`,
		DeployApprovalRequestStatusDeployed,
		id,
		DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to mark deploy approval as deployed: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to mark deploy approval as deployed: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deployment approval not found or not approved")
	}
	return nil
}
