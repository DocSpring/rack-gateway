package db

import (
	"errors"
	"fmt"
	"strings"
)

// ErrDeployApprovalNotRecordable is returned when a step can't be recorded in a deploy approval because it isn't
// approved (or was never found).
var ErrDeployApprovalNotRecordable = errors.New("deployment approval not found or not approved")

// recordableApproval matches an approval that was approved, whether or not it has expired since. A step the
// approval authorized records its result (a build, a process) even when the approval expired while the step ran, so
// extending the approval can carry on from it. Recording never authorizes anything: every new step claims (or, for
// a process start, checks again) an approved, unexpired approval first, and an exec command is recorded before it
// runs, against an unexpired approval only. Its two placeholders take recordableApprovalStatuses.
const recordableApproval = `status IN (?, ?) AND approved_at IS NOT NULL`

var recordableApprovalStatuses = []interface{}{DeployApprovalRequestStatusApproved, DeployApprovalRequestStatusExpired}

// MarkDeployApprovalRequestBuildStarted records the build created under an approval's build claim and, once the
// build has finished, its release: the real Convox API returns a new build with an empty release, which a later
// read of the build fills in. Neither is ever replaced, so a second build can't be recorded and a build read can't
// rebind the release.
func (d *Database) MarkDeployApprovalRequestBuildStarted(id int64, buildID, releaseID string) error {
	if strings.TrimSpace(buildID) == "" {
		return fmt.Errorf("build id required")
	}
	args := append([]interface{}{buildID, releaseID, id}, recordableApprovalStatuses...)
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET build_id = ?, release_id = COALESCE(NULLIF(release_id, ''), NULLIF(?::text, '')), updated_at = NOW()
         WHERE id = ? AND `+recordableApproval+`
           AND (build_id = ? OR (build_id IS NULL AND build_started_at IS NOT NULL))
           AND (?::text = '' OR NULLIF(release_id, '') IS NULL OR release_id = ?)`,
		append(args, buildID, releaseID, releaseID)...,
	)
	updated, err := updatedOneRow(res, err)
	if err != nil {
		return fmt.Errorf("failed to update build tracking: %w", err)
	}
	if !updated {
		return fmt.Errorf("deploy approval request %d was never approved, has no build claim, or has another "+
			"build or release", id)
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
         WHERE id = ? AND `+recordableApproval,
		append([]interface{}{processID, id}, recordableApprovalStatuses...)...,
	)
	if err != nil {
		return fmt.Errorf("failed to append process ID to deploy approval request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to append process ID to deploy approval request: %w", err)
	}
	if rows == 0 {
		return ErrDeployApprovalNotRecordable
	}
	return nil
}

// AppendExecCommandToDeployApprovalRequest adds an exec command to the approval's record, before the exec runs: an
// exec runs only once it's on the record. Unlike the steps recorded after they ran, this authorizes the exec, so the
// approval must still be approved and unexpired. exec_commands lists each process's commands in the order they ran.
// A process's value written as a single string (by a release before the lists) becomes the list's first item.
func (d *Database) AppendExecCommandToDeployApprovalRequest(id int64, processID, command string) error {
	if strings.TrimSpace(processID) == "" {
		return fmt.Errorf("process id required")
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command required")
	}

	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET exec_commands = jsonb_set(
               COALESCE(exec_commands, '{}'::jsonb), ARRAY[?::text],
               COALESCE(exec_commands -> ?::text, '[]'::jsonb) || to_jsonb(?::text)),
             updated_at = NOW()
         WHERE id = ? AND status = ? AND (approval_expires_at IS NULL OR approval_expires_at > NOW())`,
		processID, processID, command, id, DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to append exec command to deploy approval request: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to append exec command to deploy approval request: %w", err)
	}
	if rows == 0 {
		return ErrDeployApprovalNotRecordable
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
		return ErrDeployApprovalNotRecordable
	}
	return nil
}
