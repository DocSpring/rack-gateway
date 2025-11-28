package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type deployApprovalRequestList struct {
	DeployApprovalRequests []deployApprovalRequest `json:"deploy_approval_requests"`
}

type deployApprovalListOptions struct {
	rackFlag string
	status   string
	onlyOpen bool
	limit    int
	output   string
}

func newDeployApprovalListCommand() *cobra.Command {
	var opts deployApprovalListOptions

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List deploy approval requests",
		Long:  "List deploy approval requests with optional filtering by status.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeDeployApprovalList(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.rackFlag, "rack", "", "Rack name")
	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Filter by status (pending, approved, rejected, expired)")
	cmd.Flags().BoolVar(&opts.onlyOpen, "open", false, "Only show open (pending) requests")
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", 50, "Maximum number of results")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format (json)")

	return cmd
}

func executeDeployApprovalList(cmd *cobra.Command, opts deployApprovalListOptions) error {
	rack, err := resolveRackFlag(opts.rackFlag)
	if err != nil {
		return err
	}

	endpoint := buildDeployApprovalListEndpoint(opts)

	var result deployApprovalRequestList
	if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
		return err
	}

	if opts.output == "json" {
		return printJSON(cmd, result)
	}

	if len(result.DeployApprovalRequests) == 0 {
		fmt.Println("No deploy approval requests found.")
		return nil
	}

	return printDeployApprovalTable(result.DeployApprovalRequests)
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

	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	return endpoint
}

func printDeployApprovalTable(requests []deployApprovalRequest) error {
	fmt.Printf("%-36s  %-10s  %-20s  %-40s  %s\n",
		"ID", "STATUS", "CREATED", "MESSAGE", "TOKEN")
	fmt.Println(strings.Repeat("-", 130))

	for _, req := range requests {
		message := req.Message
		if len(message) > 40 {
			message = message[:37] + "..."
		}

		tokenName := req.TargetAPITokenName
		if tokenName == "" {
			tokenName = req.TargetAPITokenID
		}
		if len(tokenName) > 20 {
			tokenName = tokenName[:17] + "..."
		}

		fmt.Printf("%-36s  %-10s  %-20s  %-40s  %s\n",
			req.PublicID,
			req.Status,
			req.CreatedAt.Format(time.RFC3339),
			message,
			tokenName,
		)
	}

	return nil
}
