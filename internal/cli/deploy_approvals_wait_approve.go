package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
)

// errApprovalNotConfirmed is returned when the user doesn't confirm the commit of a request found by branch or by
// a short commit.
var errApprovalNotConfirmed = errors.New("not approved: the request wasn't confirmed")

// approve approves a pending request that was just shown. Unless its commit is exactly the full SHA the run was
// given (the default is the full SHA of HEAD), the request is the newest one for the branch whatever its commit,
// or the newest one whose commit merely starts with a short --commit (the gateway matches a prefix). It is then
// only approved once the user confirms its commit. That only happens in a terminal: without one, approving needs
// a full --commit up front.
func (w *deployApprovalWaiter) approve(rack string, request deployApprovalRequest) error {
	if !strings.EqualFold(w.commit, request.GitCommitHash) {
		if err := w.confirmCommit(request); err != nil {
			return err
		}
	}

	prompt := webauthn.Prompt{Summary: w.pinSummary(rack, request), Touch: approvalTouch(rack, &request, 0, 0)}
	approved, pin, err := approveDeployRequestWithPIN(w.cmd, rack, request.PublicID, w.notes, w.cachedPIN, prompt)
	if err != nil {
		return err
	}

	// Cache the PIN for subsequent approvals
	if w.cachedPIN == "" && pin != "" {
		w.cachedPIN = pin
	}

	return printApprovalSuccess(w.cmd.OutOrStdout(), approved, rack, len(w.racks) > 1)
}

// confirmCommit asks the user to confirm the full commit of a request found by branch or by a short commit (y/N).
func (w *deployApprovalWaiter) confirmCommit(request deployApprovalRequest) error {
	match := "the newest request for branch " + displayText(w.branch)
	if w.commit != "" {
		match = "the newest request for a commit starting with " + displayText(w.commit)
	}
	err := writef(w.cmd.OutOrStdout(), "\nApprove %s at commit %s, %s? [y/N]: ",
		displayText(request.App), displayText(request.GitCommitHash), match)
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
		return errApprovalNotConfirmed
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
