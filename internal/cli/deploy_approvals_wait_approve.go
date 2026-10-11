package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errBranchApprovalNotConfirmed is returned when the user doesn't confirm a request found by branch alone.
var errBranchApprovalNotConfirmed = errors.New("not approved: the request wasn't confirmed")

// approve approves a pending request that was just shown. Without --commit, the request is the newest one for
// the branch, whatever its commit, so it is only approved once the user confirms it. That only happens in a
// terminal: without one, approving by branch is refused up front.
func (w *deployApprovalWaiter) approve(rack string, request deployApprovalRequest) error {
	if w.commit == "" {
		if err := w.confirmBranchApproval(request); err != nil {
			return err
		}
	}

	approved, pin, err := approveDeployRequestWithPIN(
		w.cmd, rack, request.PublicID, w.notes, w.cachedPIN, w.pinSummary(rack, request),
	)
	if err != nil {
		return err
	}

	// Cache the PIN for subsequent approvals
	if w.cachedPIN == "" && pin != "" {
		w.cachedPIN = pin
	}

	return printApprovalSuccess(w.cmd.OutOrStdout(), approved, rack, len(w.racks) > 1)
}

// confirmBranchApproval asks the user to confirm the commit of a request found by branch alone (y/N).
func (w *deployApprovalWaiter) confirmBranchApproval(request deployApprovalRequest) error {
	err := writef(w.cmd.OutOrStdout(), "\nApprove %s at commit %s, the newest request for branch %s? [y/N]: ",
		displayText(request.App), displayText(request.GitCommitHash), displayText(w.branch))
	if err != nil {
		return err
	}

	answer, err := w.input().ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return errBranchApprovalNotConfirmed
	}
}

// input reads answers to prompts. One reader serves every prompt of the run, so typed-ahead input isn't lost.
func (w *deployApprovalWaiter) input() *bufio.Reader {
	if w.stdin == nil {
		w.stdin = bufio.NewReader(w.cmd.InOrStdin())
	}
	return w.stdin
}

// pinSummary lists, for the PIN dialog, the approval being made now and every later approval this run can make
// with the same PIN: one for each listed app on each rack that has no handled request yet.
func (w *deployApprovalWaiter) pinSummary(rack string, request deployApprovalRequest) []string {
	lines := []string{approvalSummaryLine(rack, &request)}

	target := "commit " + shortCommit(w.commit)
	if w.commit == "" {
		target = "branch " + displayText(w.branch)
	}
	for _, r := range w.racks {
		for _, app := range w.apps.names {
			if !w.isHandled(r, app) {
				line := fmt.Sprintf("%s on %s, %s (when its request appears)", displayText(app), displayText(r), target)
				lines = append(lines, line)
			}
		}
	}
	return lines
}
