package circleci

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client handles CircleCI API interactions.
type Client struct {
	APIToken string
	BaseURL  string
	client   *http.Client
}

// NewClient creates a new CircleCI API client.
func NewClient(apiToken string) *Client {
	return &Client{
		APIToken: apiToken,
		BaseURL:  "https://circleci.com/api/v2",
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ApprovalMetadata contains CircleCI-specific approval metadata.
type ApprovalMetadata struct {
	WorkflowID      string `json:"workflow_id"`
	ApprovalJobName string `json:"approval_job_name"`
}

// ApproveJob approves a pending approval job in a CircleCI workflow.
func (c *Client) ApproveJob(workflowID, jobName string) error {
	if workflowID == "" {
		return fmt.Errorf("workflow_id is required")
	}
	if jobName == "" {
		return fmt.Errorf("job_name is required")
	}

	// First, get the workflow to find the approval job ID
	workflow, err := c.getWorkflow(workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find the approval job by name
	jobID, err := findApprovalJobID(workflow, jobName)
	if err != nil {
		return fmt.Errorf("failed to find approval job: %w", err)
	}

	// Approve the job
	url := fmt.Sprintf("%s/workflow/%s/approve/%s", c.BaseURL, workflowID, jobID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Circle-Token", c.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("CircleCI API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

type workflowResponse struct {
	ID   string `json:"id"`
	Jobs []struct {
		ID              string    `json:"id"`
		Name            string    `json:"name"`
		Type            string    `json:"type"`
		ApprovalRequest *struct{} `json:"approval_request_id,omitempty"`
		Status          string    `json:"status"`
	} `json:"jobs"`
}

func (c *Client) getWorkflow(workflowID string) (*workflowResponse, error) {
	url := fmt.Sprintf("%s/workflow/%s/job", c.BaseURL, workflowID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Circle-Token", c.APIToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("CircleCI API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Items []struct {
			ID              string    `json:"id"`
			Name            string    `json:"name"`
			Type            string    `json:"type"`
			ApprovalRequest *struct{} `json:"approval_request_id,omitempty"`
			Status          string    `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse workflow response: %w", err)
	}

	workflow := &workflowResponse{
		ID: workflowID,
		Jobs: make([]struct {
			ID              string    `json:"id"`
			Name            string    `json:"name"`
			Type            string    `json:"type"`
			ApprovalRequest *struct{} `json:"approval_request_id,omitempty"`
			Status          string    `json:"status"`
		}, len(result.Items)),
	}

	for i, item := range result.Items {
		workflow.Jobs[i] = struct {
			ID              string    `json:"id"`
			Name            string    `json:"name"`
			Type            string    `json:"type"`
			ApprovalRequest *struct{} `json:"approval_request_id,omitempty"`
			Status          string    `json:"status"`
		}{
			ID:              item.ID,
			Name:            item.Name,
			Type:            item.Type,
			ApprovalRequest: item.ApprovalRequest,
			Status:          item.Status,
		}
	}

	return workflow, nil
}

func findApprovalJobID(workflow *workflowResponse, jobName string) (string, error) {
	for _, job := range workflow.Jobs {
		if job.Name == jobName && job.Type == "approval" {
			return job.ID, nil
		}
	}
	return "", fmt.Errorf("approval job %q not found in workflow", jobName)
}

// ValidateMetadata validates that the metadata contains required CircleCI fields.
func ValidateMetadata(metadata map[string]interface{}) error {
	if metadata == nil {
		return fmt.Errorf("ci_metadata is required for circleci provider")
	}

	workflowID, ok := metadata["workflow_id"].(string)
	if !ok || strings.TrimSpace(workflowID) == "" {
		return fmt.Errorf("ci_metadata.workflow_id is required")
	}

	approvalJobName, ok := metadata["approval_job_name"].(string)
	if !ok || strings.TrimSpace(approvalJobName) == "" {
		return fmt.Errorf("ci_metadata.approval_job_name is required")
	}

	return nil
}

// ParseMetadata converts map to ApprovalMetadata struct.
func ParseMetadata(metadata map[string]interface{}) (*ApprovalMetadata, error) {
	if err := ValidateMetadata(metadata); err != nil {
		return nil, err
	}

	return &ApprovalMetadata{
		WorkflowID:      metadata["workflow_id"].(string),
		ApprovalJobName: metadata["approval_job_name"].(string),
	}, nil
}
