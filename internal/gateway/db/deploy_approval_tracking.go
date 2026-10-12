package db

import (
	"fmt"
	"strings"
)

// MarkDeployApprovalRequestBuildStarted records the build created under an approval's build claim and, once the
// build has finished, its release: the real Convox API returns a new build with an empty release, which a later
// read of the build fills in. Neither is ever replaced, so a second build can't be recorded and a build read can't
// rebind the release.
func (d *Database) MarkDeployApprovalRequestBuildStarted(id int64, buildID, releaseID string) error {
	if strings.TrimSpace(buildID) == "" {
		return fmt.Errorf("build id required")
	}
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET build_id = ?, release_id = COALESCE(NULLIF(release_id, ''), NULLIF(?::text, '')), updated_at = NOW()
         WHERE id = ? AND status = ?
           AND (build_id = ? OR (build_id IS NULL AND build_started_at IS NOT NULL))
           AND (?::text = '' OR NULLIF(release_id, '') IS NULL OR release_id = ?)`,
		buildID, releaseID, id, DeployApprovalRequestStatusApproved, buildID, releaseID, releaseID,
	)
	updated, err := updatedOneRow(res, err)
	if err != nil {
		return fmt.Errorf("failed to update build tracking: %w", err)
	}
	if !updated {
		return fmt.Errorf("deploy approval request %d is not approved, has no build claim, or has another build "+
			"or release", id)
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

// MarkDeployApprovalAsDeployed marks a deploy approval request as deployed after successful promotion. The
// promote was authorized while the approval was open; the approval may have expired while the promote ran (up to
// 10 minutes), and still records that it was deployed.
func (d *Database) MarkDeployApprovalAsDeployed(id int64) error {
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET status = ?, updated_at = NOW()
         WHERE id = ? AND status IN (?, ?) AND release_id IS NOT NULL`,
		DeployApprovalRequestStatusDeployed,
		id,
		DeployApprovalRequestStatusApproved,
		DeployApprovalRequestStatusExpired,
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
