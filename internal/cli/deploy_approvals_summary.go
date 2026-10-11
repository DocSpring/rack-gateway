package cli

import "fmt"

// shortCommitLength is how much of a commit SHA one-line summaries show.
const shortCommitLength = 12

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

// approvalTarget names what a request deploys: its app and short commit.
func approvalTarget(req *deployApprovalRequest) string {
	commit := "commit " + shortCommit(req.GitCommitHash)
	if req.App == "" {
		return commit
	}
	return displayText(req.App) + ", " + commit
}

// approvalTouch says what the security key touch for an approval is for, e.g. "approve api on staging (2 of 4)".
// total is how many approvals the command makes, when it knows (0 otherwise).
func approvalTouch(rack string, req *deployApprovalRequest, current, total int) string {
	app := displayText(req.App)
	if app == "" {
		app = "request " + req.PublicID
	}
	touch := fmt.Sprintf("approve %s on %s", app, displayText(rack))
	if total > 1 {
		touch += fmt.Sprintf(" (%d of %d)", current, total)
	}
	return touch
}

// approvalSummaryLine describes one approval for the PIN dialog: the app, the rack, the commit and the message.
func approvalSummaryLine(rack string, req *deployApprovalRequest) string {
	app := displayText(req.App)
	if app == "" {
		app = "request " + req.PublicID
	}
	return fmt.Sprintf(
		"%s on %s, commit %s: \"%s\"", app, displayText(rack), shortCommit(req.GitCommitHash), displayText(req.Message),
	)
}

// approvalSummary describes the approvals for the PIN dialog, one line per request.
func approvalSummary(requests []rackApproval) []string {
	lines := make([]string, 0, len(requests))
	for _, r := range requests {
		lines = append(lines, approvalSummaryLine(r.rack, r.req))
	}
	return lines
}
