package cli

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cli/pinentry"
)

const (
	// shortCommitLength is how much of a commit SHA one-line summaries show.
	shortCommitLength = 7
	// maxBranchRunes keeps a long branch name from pushing the app and rack out of a PIN dialog line, which the
	// dialog cuts at 160 characters.
	maxBranchRunes = 80
)

// shortCommit abbreviates a commit SHA for one-line summaries.
func shortCommit(hash string) string {
	runes := []rune(displayText(hash))
	if len(runes) == 0 {
		return "(none)"
	}
	if len(runes) > shortCommitLength {
		runes = runes[:shortCommitLength]
	}
	return string(runes)
}

// approvalBranch is the request's branch for one-line summaries, cut to maxBranchRunes, or "" when it has none.
func approvalBranch(req *deployApprovalRequest) string {
	return pinentry.SingleLine(displayText(strings.TrimSpace(req.GitBranch)), maxBranchRunes)
}

// approvalApp names the request's app, or the request itself when it has none.
func approvalApp(req *deployApprovalRequest) string {
	if app := displayText(req.App); app != "" {
		return app
	}
	return "request " + displayText(req.PublicID)
}

// approvalTarget names what a request deploys: its app and short commit.
func approvalTarget(req *deployApprovalRequest) string {
	commit := "commit " + shortCommit(req.GitCommitHash)
	if req.App == "" {
		return commit
	}
	return displayText(req.App) + ", " + commit
}

// approvalTouch says what the security key touch for an approval is for. The branch comes first, since it's what
// the person approving recognizes: "approve feature/fix-login (api on staging, 2 of 4)", or "approve api on staging
// (2 of 4)" for a request without one. Later touches of a run come without a PIN dialog, so this is all they show.
// total is how many approvals the command makes, when it knows (0 otherwise).
func approvalTouch(rack string, req *deployApprovalRequest, current, total int) string {
	subject := approvalApp(req) + " on " + displayText(rack)
	var details []string
	if branch := approvalBranch(req); branch != "" {
		details = append(details, subject)
		subject = branch
	}
	if total > 1 {
		details = append(details, fmt.Sprintf("%d of %d", current, total))
	}
	touch := "approve " + subject
	if len(details) > 0 {
		touch += " (" + strings.Join(details, ", ") + ")"
	}
	return touch
}

// approvalSummaryLine describes one approval for the PIN dialog: the branch first (see approvalTouch), then the app,
// the rack, the short commit and the message, e.g. `feature/fix-login: api on staging, commit 36a302d, "Fix login"`.
func approvalSummaryLine(rack string, req *deployApprovalRequest) string {
	line := fmt.Sprintf(
		"%s on %s, commit %s, \"%s\"",
		approvalApp(req), displayText(rack), shortCommit(req.GitCommitHash), displayText(req.Message),
	)
	if branch := approvalBranch(req); branch != "" {
		return branch + ": " + line
	}
	return line
}

// approvalSummary describes the approvals for the PIN dialog, one line per request.
func approvalSummary(requests []rackApproval) []string {
	lines := make([]string, 0, len(requests))
	for _, r := range requests {
		lines = append(lines, approvalSummaryLine(r.rack, r.req))
	}
	return lines
}
