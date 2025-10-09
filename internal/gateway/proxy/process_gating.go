package proxy

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// trackProcessCreation records a process created via the gateway.
func (h *Handler) trackProcessCreation(r *http.Request, path, processID, email string) {
	if h.database == nil {
		return
	}

	// Extract app name from path: /apps/{app}/services/{service}/processes
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return
	}
	app := parts[1]

	// Get creator info
	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		return
	}

	var userID, tokenID *int64
	if authUser.IsAPIToken {
		tokenID = authUser.TokenID
	} else {
		if user, err := h.rbacManager.GetUserWithID(email); err == nil && user != nil {
			userID = &user.ID
		}
	}

	// Extract release ID from "Release" header (Convox API convention)
	releaseID := strings.TrimSpace(r.Header.Get("Release"))

	// Get approval tracker from context (if this was approved)
	approvalTracker := getDeployApprovalTracker(r.Context())
	if approvalTracker != nil && releaseID != "" {
		// Validate that the release ID matches the approved deployment
		if approvalTracker.releaseID != "" && approvalTracker.releaseID != releaseID {
			log.Printf(`{"level":"warn","event":"process_start_release_mismatch","process_id":%q,"approved_release":%q,"requested_release":%q}`,
				processID, approvalTracker.releaseID, releaseID)
			// Don't track this process since it's not for the approved release
			return
		}
	}

	// Create process record with release ID
	if err := h.database.CreateProcess(processID, app, releaseID, userID, tokenID); err != nil {
		log.Printf(`{"level":"error","event":"process_tracking_failed","process_id":%q,"error":%q}`, processID, err.Error())
	}
}

// checkProcessExec gates process exec with command allowlist and approval checks.
// Returns (allowed, error message).
func (h *Handler) checkProcessExec(r *http.Request, authUser *auth.AuthUser, path string, approvalTracker *deployApprovalTracker) (bool, string) {
	if h.database == nil {
		return true, "" // No database, no gating
	}

	// Extract process ID from path: /apps/{app}/processes/{pid}/exec
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return false, "invalid process path"
	}
	processID := parts[3]

	// Get command from header or query parameter (Convox CLI passes it as "Command" header)
	command := strings.TrimSpace(r.Header.Get("Command"))
	if command == "" {
		command = strings.TrimSpace(r.URL.Query().Get("command"))
	}
	log.Printf(`{"level":"debug","event":"checkProcessExec_command","process_id":%q,"command":%q}`, processID, command)
	if command == "" {
		return false, "no command specified"
	}

	// Check if process exists and was created by this user/token
	process, err := h.database.GetProcess(processID)
	if err != nil {
		log.Printf(`{"level":"error","event":"process_lookup_failed","process_id":%q,"error":%q}`, processID, err.Error())
		return false, "failed to verify process ownership"
	}
	log.Printf(`{"level":"debug","event":"checkProcessExec_process","process_id":%q,"process_found":%v}`, processID, process != nil)
	if process == nil {
		// Process not tracked - allow for regular users but deny for API tokens with -with-approval permission
		// (This handles processes created outside the gateway, like existing app processes)
		if authUser.IsAPIToken {
			// Check if they have the gated permission (exec-with-approval)
			if tokenHasPermission(authUser.Permissions, "convox:process:exec-with-approval") {
				return false, "cannot exec into untracked processes (not created via gateway)"
			}
		}
		// Regular users or tokens without -with-approval can exec into any process
		log.Printf(`{"level":"debug","event":"checkProcessExec_allowing_untracked","process_id":%q}`, processID)
		return true, ""
	}

	// Verify ownership
	var isOwner bool
	if authUser.IsAPIToken && authUser.TokenID != nil {
		isOwner = process.CreatedByAPITokenID != nil && *process.CreatedByAPITokenID == *authUser.TokenID
	} else {
		if user, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && user != nil {
			isOwner = process.CreatedByUserID != nil && *process.CreatedByUserID == user.ID
		}
	}

	log.Printf(`{"level":"debug","event":"checkProcessExec_ownership","process_id":%q,"is_owner":%v}`, processID, isOwner)
	if !isOwner {
		return false, "can only exec into processes you created"
	}

	// For API tokens, check permissions directly from the token
	// For users, check RBAC
	var hasStandardExec, hasExecWithApproval bool
	if authUser.IsAPIToken {
		hasStandardExec = h.hasAPITokenPermission(authUser, rbac.ResourceProcess, rbac.ActionExec)
		hasExecWithApproval = tokenHasPermission(authUser.Permissions, "convox:process:exec-with-approval")
	} else {
		hasStandardExec, _ = h.rbacManager.Enforce(authUser.Email, rbac.ScopeConvox, rbac.ResourceProcess, rbac.ActionExec)
		hasExecWithApproval, _ = h.rbacManager.Enforce(authUser.Email, rbac.ScopeConvox, rbac.ResourceProcess, rbac.ActionExec)
	}

	// Check if user has standard exec permission - if so, allow immediately
	if hasStandardExec {
		log.Printf(`{"level":"debug","event":"checkProcessExec_has_standard_exec","process_id":%q}`, processID)
		return true, ""
	}

	// Check if user has exec-with-approval permission - requires command allowlist and deploy approval
	if hasExecWithApproval {
		log.Printf(`{"level":"debug","event":"checkProcessExec_has_exec_with-approval","process_id":%q}`, processID)
		// Check command against allowlist
		approvedCommands, err := h.database.GetApprovedCommands()
		if err != nil {
			log.Printf(`{"level":"error","event":"approved_commands_lookup_failed","error":%q}`, err.Error())
			return false, "failed to check command allowlist"
		}

		log.Printf(`{"level":"debug","event":"checkProcessExec_approved_commands","process_id":%q,"approved_commands":%v,"command":%q}`, processID, approvedCommands, command)
		commandAllowed := false
		for _, approved := range approvedCommands {
			if command == approved {
				commandAllowed = true
				break
			}
		}

		if !commandAllowed {
			log.Printf(`{"level":"debug","event":"checkProcessExec_command_not_allowed","process_id":%q,"command":%q}`, processID, command)
			return false, fmt.Sprintf("command %q not in approved commands list", command)
		}

		// Check deploy approval if required
		if approvalTracker == nil {
			log.Printf(`{"level":"debug","event":"checkProcessExec_no_approval_tracker","process_id":%q}`, processID)
			return false, "exec requires an approved deploy approval request"
		}

		// Verify process release ID matches the approved release ID
		if process != nil && process.ReleaseID != "" {
			if approvalTracker.releaseID != "" && process.ReleaseID != approvalTracker.releaseID {
				log.Printf(`{"level":"warn","event":"checkProcessExec_release_mismatch","process_id":%q,"process_release":%q,"approved_release":%q}`,
					processID, process.ReleaseID, approvalTracker.releaseID)
				return false, fmt.Sprintf("process release %s does not match approved release %s", process.ReleaseID, approvalTracker.releaseID)
			}
		}

		// Update process with command and approval request ID
		if err := h.database.UpdateProcessCommand(processID, command, &approvalTracker.request.ID); err != nil {
			log.Printf(`{"level":"error","event":"process_command_update_failed","process_id":%q,"error":%q}`, processID, err.Error())
			// Don't fail the request if we can't update the tracking
		}

		log.Printf(`{"level":"debug","event":"checkProcessExec_allowed","process_id":%q}`, processID)
		return true, ""
	}

	// No exec permission
	log.Printf(`{"level":"debug","event":"checkProcessExec_no_permission","process_id":%q}`, processID)
	return false, "permission denied"
}

// checkProcessTerminate gates process termination to only processes created by the requester.
// Returns (allowed, error message).
func (h *Handler) checkProcessTerminate(r *http.Request, authUser *auth.AuthUser, path string, approvalTracker *deployApprovalTracker) (bool, string) {
	if h.database == nil {
		return true, "" // No database, no gating
	}

	// Extract process ID from path: /apps/{app}/processes/{pid}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return false, "invalid process path"
	}
	processID := parts[3]

	// Get process
	process, err := h.database.GetProcess(processID)
	if err != nil {
		log.Printf(`{"level":"error","event":"process_lookup_failed","process_id":%q,"error":%q}`, processID, err.Error())
		return false, "failed to verify process ownership"
	}
	if process == nil {
		// Process not tracked - allow for regular users but deny for API tokens with -with-approval permission
		if authUser.IsAPIToken {
			// Check if they have the gated permission (terminate-with-approval)
			if allowed, _ := h.rbacManager.Enforce(authUser.Email, rbac.ScopeConvox, rbac.ResourceProcess, rbac.ActionTerminate); allowed {
				return false, "cannot terminate untracked processes (not created via gateway)"
			}
		}
		// Regular users or tokens without -with-approval can terminate any process
		return true, ""
	}

	// Verify ownership
	var isOwner bool
	if authUser.IsAPIToken && authUser.TokenID != nil {
		isOwner = process.CreatedByAPITokenID != nil && *process.CreatedByAPITokenID == *authUser.TokenID
	} else {
		if user, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && user != nil {
			isOwner = process.CreatedByUserID != nil && *process.CreatedByUserID == user.ID
		}
	}

	if !isOwner {
		return false, "can only terminate processes you created"
	}

	// If using -with-approval permission, verify process release ID matches approved release
	if approvalTracker != nil && process.ReleaseID != "" {
		if approvalTracker.releaseID != "" && process.ReleaseID != approvalTracker.releaseID {
			log.Printf(`{"level":"warn","event":"checkProcessTerminate_release_mismatch","process_id":%q,"process_release":%q,"approved_release":%q}`,
				processID, process.ReleaseID, approvalTracker.releaseID)
			return false, fmt.Sprintf("process release %s does not match approved release %s", process.ReleaseID, approvalTracker.releaseID)
		}
	}

	// Mark process as terminated
	if err := h.database.MarkProcessTerminated(processID); err != nil {
		log.Printf(`{"level":"error","event":"process_termination_tracking_failed","process_id":%q,"error":%q}`, processID, err.Error())
		// Don't fail the request if we can't update the tracking
	}

	return true, ""
}
