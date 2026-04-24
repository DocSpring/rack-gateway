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

	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Filter by status (pending, approved, rejected, expired)")
	cmd.Flags().BoolVar(&opts.onlyOpen, "open", false, "Only show open (pending) requests")
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", 50, "Maximum number of results per rack")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format (json)")

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

	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	return endpoint
}

func printDeployApprovalTableWithRack(
	requests []deployApprovalRequest, rackMap map[string]string, showRack bool,
) error {
	if showRack {
		fmt.Printf("%-12s  %-36s  %-10s  %-20s  %-30s  %s\n",
			"RACK", "ID", "STATUS", "CREATED", "MESSAGE", "TOKEN")
		fmt.Println(strings.Repeat("-", 145))
	} else {
		fmt.Printf("%-36s  %-10s  %-20s  %-30s  %s\n",
			"ID", "STATUS", "CREATED", "MESSAGE", "TOKEN")
		fmt.Println(strings.Repeat("-", 120))
	}

	for _, req := range requests {
		message := req.Message
		if len(message) > 30 {
			message = message[:27] + "..."
		}

		tokenName := req.TargetAPITokenName
		if tokenName == "" {
			tokenName = req.TargetAPITokenID
		}
		if len(tokenName) > 15 {
			tokenName = tokenName[:12] + "..."
		}

		if showRack {
			rack := rackMap[req.PublicID]
			fmt.Printf("%-12s  %-36s  %-10s  %-20s  %-30s  %s\n",
				rack,
				req.PublicID,
				req.Status,
				req.CreatedAt.Format(time.RFC3339),
				message,
				tokenName,
			)
		} else {
			fmt.Printf("%-36s  %-10s  %-20s  %-30s  %s\n",
				req.PublicID,
				req.Status,
				req.CreatedAt.Format(time.RFC3339),
				message,
				tokenName,
			)
		}
	}

	return nil
}
