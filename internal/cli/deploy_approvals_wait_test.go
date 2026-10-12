package cli

import (
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestWaitShowsTheCommitOfEachRequest(t *testing.T) {
	gateway := startFakeDeployGateway(t,
		shownRequest("p-web", "web", "pending"),
		shownRequest("a-worker", "worker", "approved"),
	)
	waiter := newDeployApprovalWaiter(gateway.command(""), deployApprovalWaitConfig{
		racks: []string{"staging"}, apps: approvalApps{names: []string{"web", "worker"}},
		commit: appsTestCommit, pollInterval: time.Millisecond,
	})
	defer waiter.waitForSound()

	done, err := waiter.pollNextRack()
	require.NoError(t, err)
	require.True(t, done)

	out := gateway.output.String()
	require.Contains(t, out, "Deploy Approval Request Found")
	require.Contains(t, out, appsTestCommit, "the pending request's details show its full commit")
	require.Contains(t, out, "✓ Already approved on rack staging: a-worker (worker, commit 36a302d01234)")
	require.Empty(t, gateway.seen(), "without --approve, wait only shows requests")
}

// With --commit, wait --approve approves without asking: the input is empty, so a prompt would decline.
func TestWaitApproveShowsTheRequestAndSummarizesTheCommit(t *testing.T) {
	gateway := startFakeDeployGateway(t, shownRequest("p-web", "web", "pending"))
	waiter := newDeployApprovalWaiter(gateway.command(""), deployApprovalWaitConfig{
		racks: []string{"staging"}, apps: approvalApps{names: []string{"web"}},
		commit: appsTestCommit, pollInterval: time.Millisecond, autoApprove: true,
	})
	defer waiter.waitForSound()

	done, err := waiter.pollNextRack()
	require.NoError(t, err)
	require.True(t, done)

	approvals := gateway.seen()
	require.Len(t, approvals, 1)
	for _, want := range []string{"p-web", "web", appsTestCommit, "Deploy web", "main", "ci-deployer"} {
		require.Contains(t, approvals[0].outputBefore, want, "the request is shown before it is approved")
	}

	require.NoError(t, waiter.printSummary())
	require.Contains(t, gateway.output.String(), "p-web (web, commit 36a302d01234) approved")
	require.Contains(t, gateway.output.String(), "staging  web  p-web  commit 36a302d01234")
}

// The newest request for a branch can be for any commit, so approving by branch alone needs a confirmation.
func TestWaitApproveByBranchNeedsConfirmation(t *testing.T) {
	answers := map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false}
	for answer, wantApproved := range answers {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			gateway := startFakeDeployGateway(t, shownRequest("p-web", "web", "pending"))
			waiter := newDeployApprovalWaiter(gateway.command(answer), deployApprovalWaitConfig{
				racks: []string{"staging"}, apps: approvalApps{names: []string{"web"}},
				branch: "main", pollInterval: time.Millisecond, autoApprove: true,
			})
			defer waiter.waitForSound()

			_, err := waiter.pollNextRack()

			prompt := "Approve web at commit " + appsTestCommit + ", the newest request for branch main? [y/N]"
			require.Contains(t, gateway.output.String(), prompt)
			if wantApproved {
				require.NoError(t, err)
				require.Len(t, gateway.seen(), 1)
			} else {
				require.ErrorIs(t, err, errApprovalNotConfirmed)
				require.Empty(t, gateway.seen())
			}
		})
	}
}

// The gateway matches a short --commit as a prefix, so the request it finds can be for another commit that starts
// the same way: approving it needs a confirmation of the full commit.
func TestWaitApproveByShortCommitNeedsConfirmation(t *testing.T) {
	answers := map[string]bool{"y\n": true, "\n": false, "": false}
	for answer, wantApproved := range answers {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			gateway := startFakeDeployGateway(t, shownRequest("p-web", "web", "pending"))
			waiter := newDeployApprovalWaiter(gateway.command(answer), deployApprovalWaitConfig{
				racks: []string{"staging"}, apps: approvalApps{names: []string{"web"}},
				commit: appsTestCommit[:7], pollInterval: time.Millisecond, autoApprove: true,
			})
			defer waiter.waitForSound()

			_, err := waiter.pollNextRack()

			prompt := "Approve web at commit " + appsTestCommit +
				", the newest request for a commit starting with 36a302d? [y/N]"
			require.Contains(t, gateway.output.String(), prompt)
			if wantApproved {
				require.NoError(t, err)
				require.Len(t, gateway.seen(), 1)
			} else {
				require.ErrorIs(t, err, errApprovalNotConfirmed)
				require.Empty(t, gateway.seen())
			}
		})
	}
}

func TestWaitPINSummaryListsEveryApprovalTheRunCanMake(t *testing.T) {
	apps := approvalApps{names: []string{"web", "worker"}}
	web := shownRequest("p-web", "web", "pending")

	waiter := newDeployApprovalWaiter(&cobra.Command{}, deployApprovalWaitConfig{
		racks: []string{"staging", "us"}, apps: apps, commit: appsTestCommit, autoApprove: true,
	})
	waiter.markHandled("staging", web)
	waiter.markHandled("us", shownRequest("a-worker", "worker", "approved"))
	require.Equal(t, []string{
		`web on staging, commit 36a302d01234: "Deploy web"`,
		"worker on staging, commit 36a302d01234 (when its request appears)",
		"web on us, commit 36a302d01234 (when its request appears)",
	}, waiter.pinSummary("staging", web))

	byBranch := newDeployApprovalWaiter(&cobra.Command{}, deployApprovalWaitConfig{
		racks: []string{"staging"}, apps: apps, branch: "main", autoApprove: true,
	})
	byBranch.markHandled("staging", web)
	require.Equal(t, []string{
		`web on staging, commit 36a302d01234: "Deploy web"`,
		"worker on staging, branch main (when its request appears)",
	}, byBranch.pinSummary("staging", web))
}
