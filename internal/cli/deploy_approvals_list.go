package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type deployApprovalListOptions struct {
	status   string
	onlyOpen bool
	limit    int
	output   string
	app      string
	commit   string
}

func newDeployApprovalListCommand() *cobra.Command {
	var opts deployApprovalListOptions

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List deploy approval requests",
		Long:  "List deploy approval requests with optional filtering by status, app and commit.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeDeployApprovalList(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Filter by status (pending, approved, rejected, expired)")
	cmd.Flags().BoolVar(&opts.onlyOpen, "open", false, "Only show open (pending) requests")
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", 50, "Maximum number of results per rack")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format (json)")
	cmd.Flags().StringVarP(&opts.app, "app", "a", "", "Only show requests for this app")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Only show requests for this commit (full SHA or prefix)")

	return cmd
}

func executeDeployApprovalList(cmd *cobra.Command, opts deployApprovalListOptions) error {
	racks, err := resolveRacks()
	if err != nil {
		return err
	}

	endpoint := buildDeployApprovalListEndpoint(opts)
	showRack := len(racks) > 1

	var allRequests []deployApprovalRequest
	rackMap := make(map[string]string) // publicID -> rack

	for _, rack := range racks {
		var result deployApprovalRequestList
		if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
			return rackScopedError(rack, err, len(racks))
		}
		for _, req := range result.DeployApprovalRequests {
			rackMap[req.PublicID] = rack
			allRequests = append(allRequests, req)
		}
	}

	if opts.output == "json" {
		return printJSON(cmd, deployApprovalRequestList{DeployApprovalRequests: allRequests})
	}

	if len(allRequests) == 0 {
		fmt.Println("No deploy approval requests found.")
		return nil
	}

	return printDeployApprovalTableWithRack(allRequests, rackMap, showRack)
}

func buildDeployApprovalListEndpoint(opts deployApprovalListOptions) string {
	endpoint := "/deploy-approval-requests"
	params := url.Values{}

	if opts.status != "" {
		params.Set("status", opts.status)
	}
	if opts.onlyOpen {
		params.Set("only_open", "true")
	}
	if opts.limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.limit))
	}
	if app := strings.TrimSpace(opts.app); app != "" {
		params.Set("app", app)
	}
	if commit := strings.TrimSpace(opts.commit); commit != "" {
		params.Set("git_commit", commit)
	}

	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	return endpoint
}

// printDeployApprovalTableWithRack prints one request per line. The message comes last and is never cut, so
// "Deploy <app> <sha7> to <env>" stays readable for people and agents.
func printDeployApprovalTableWithRack(
	requests []deployApprovalRequest, rackMap map[string]string, showRack bool,
) error {
	const format = "%-36s  %-9s  %-12s  %-7s  %-20s  %-18s  %s\n"
	if showRack {
		fmt.Printf("%-12s  "+format, "RACK", "ID", "STATUS", "APP", "COMMIT", "CREATED", "TOKEN", "MESSAGE")
	} else {
		fmt.Printf(format, "ID", "STATUS", "APP", "COMMIT", "CREATED", "TOKEN", "MESSAGE")
	}

	for _, req := range requests {
		tokenName := req.TargetAPITokenName
		if tokenName == "" {
			tokenName = req.TargetAPITokenID
		}
		if len(tokenName) > 18 {
			tokenName = tokenName[:15] + "..."
		}
		commit := req.GitCommitHash
		if len(commit) > 7 {
			commit = commit[:7]
		}
		columns := []interface{}{
			req.PublicID, req.Status, req.App, commit, req.CreatedAt.Format(time.RFC3339), tokenName, req.Message,
		}
		if showRack {
			fmt.Printf("%-12s  "+format, append([]interface{}{rackMap[req.PublicID]}, columns...)...)
		} else {
			fmt.Printf(format, columns...)
		}
	}

	return nil
}
