package cli

import (
	"fmt"
	"io"
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
		return writeLine(cmd.OutOrStdout(), "No deploy approval requests found.")
	}

	return printDeployApprovalTableWithRack(cmd.OutOrStdout(), allRequests, rackMap, showRack)
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
// "Deploy <app> <sha7> to <env>" stays readable for people and agents. Every column but the rack and the time
// comes from the gateway, so each goes through displayText.
func printDeployApprovalTableWithRack(
	out io.Writer, requests []deployApprovalRequest, rackMap map[string]string, showRack bool,
) error {
	const format = "%-36s  %-9s  %-12s  %-7s  %-20s  %-18s  %s\n"
	header := []interface{}{"ID", "STATUS", "APP", "COMMIT", "CREATED", "TOKEN", "MESSAGE"}
	if err := writeTableRow(out, format, header, "RACK", showRack); err != nil {
		return err
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
			displayText(req.PublicID), displayText(req.Status), displayText(req.App), displayText(commit),
			req.CreatedAt.Format(time.RFC3339), displayText(tokenName), displayText(req.Message),
		}
		if err := writeTableRow(out, format, columns, rackMap[req.PublicID], showRack); err != nil {
			return err
		}
	}

	return nil
}

// writeTableRow writes one row of the table, starting with the rack column when showRack is set.
func writeTableRow(out io.Writer, format string, columns []interface{}, rack string, showRack bool) error {
	if showRack {
		return writef(out, "%-12s  "+format, append([]interface{}{rack}, columns...)...)
	}
	return writef(out, format, columns...)
}
