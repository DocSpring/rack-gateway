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

// doRequest performs an HTTP request to the CircleCI API with standard error handling.
func (c *Client) doRequest(method, url string) ([]byte, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Circle-Token", c.APIToken)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

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

	return body, nil
}

// ApprovalMetadata contains CircleCI-specific approval metadata.
type ApprovalMetadata struct {
	WorkflowID      string `json:"workflow_id"`
	PipelineNumber  string `json:"pipeline_number"`
	ApprovalJobName string `json:"approval_job_name"`
}

// ApproveJob approves a pending approval job in a CircleCI workflow.
// It handles workflow reruns by finding the latest workflow with the same name in the pipeline.
func (c *Client) ApproveJob(workflowID, pipelineNumber, jobName string) error {
	if workflowID == "" {
		return fmt.Errorf("workflow_id is required")
	}
	if pipelineNumber == "" {
		return fmt.Errorf("pipeline_number is required")
	}
	if jobName == "" {
		return fmt.Errorf("job_name is required")
	}

	// Get the workflow details to find its name
	originalWorkflow, err := c.getWorkflowDetails(workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow details: %w", err)
	}

	// Find the latest workflow with the same name in the pipeline
	latestWorkflowID, err := c.findLatestWorkflowByName(pipelineNumber, originalWorkflow.Name)
	if err != nil {
		return fmt.Errorf("failed to find latest workflow: %w", err)
	}

	// Get the workflow jobs to find the approval job ID
	workflow, err := c.getWorkflow(latestWorkflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find the approval job by name
	jobID, err := findApprovalJobID(workflow, jobName)
	if err != nil {
		return fmt.Errorf("failed to find approval job: %w", err)
	}

	// Approve the job
	url := fmt.Sprintf("%s/workflow/%s/approve/%s", c.BaseURL, latestWorkflowID, jobID)
	if _, err := c.doRequest(http.MethodPost, url); err != nil {
		return err
	}

	return nil
}

type workflowResponse struct {
	ID   string `json:"id"`
	Jobs []struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Type            string  `json:"type"`
		ApprovalRequest *string `json:"approval_request_id,omitempty"`
		Status          string  `json:"status"`
	} `json:"jobs"`
}

func (c *Client) getWorkflow(workflowID string) (*workflowResponse, error) {
	url := fmt.Sprintf("%s/workflow/%s/job", c.BaseURL, workflowID)
	body, err := c.doRequest(http.MethodGet, url)
	if err != nil {
		return nil, err
	}

	var result struct {
		Items []struct {
			ID              string  `json:"id"`
			Name            string  `json:"name"`
			Type            string  `json:"type"`
			ApprovalRequest *string `json:"approval_request_id,omitempty"`
			Status          string  `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse workflow response: %w", err)
	}

	workflow := &workflowResponse{
		ID: workflowID,
		Jobs: make([]struct {
			ID              string  `json:"id"`
			Name            string  `json:"name"`
			Type            string  `json:"type"`
			ApprovalRequest *string `json:"approval_request_id,omitempty"`
			Status          string  `json:"status"`
		}, len(result.Items)),
	}

	for i, item := range result.Items {
		workflow.Jobs[i] = struct {
			ID              string  `json:"id"`
			Name            string  `json:"name"`
			Type            string  `json:"type"`
			ApprovalRequest *string `json:"approval_request_id,omitempty"`
			Status          string  `json:"status"`
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

type workflowDetails struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProjectID  string `json:"project_id"`
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

// getWorkflowDetails fetches workflow details including its name.
func (c *Client) getWorkflowDetails(workflowID string) (*workflowDetails, error) {
	url := fmt.Sprintf("%s/workflow/%s", c.BaseURL, workflowID)
	body, err := c.doRequest(http.MethodGet, url)
	if err != nil {
		return nil, err
	}

	var details workflowDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("failed to parse workflow details: %w", err)
	}

	return &details, nil
}

type pipelineWorkflow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// findLatestWorkflowByName finds the most recent workflow with the given name in a pipeline.
func (c *Client) findLatestWorkflowByName(pipelineNumber, workflowName string) (string, error) {
	url := fmt.Sprintf("%s/pipeline/%s/workflow", c.BaseURL, pipelineNumber)
	body, err := c.doRequest(http.MethodGet, url)
	if err != nil {
		return "", err
	}

	var result struct {
		Items []pipelineWorkflow `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse pipeline workflows: %w", err)
	}

	// Find the latest workflow with matching name (workflows are ordered by created_at descending)
	for _, wf := range result.Items {
		if wf.Name == workflowName {
			return wf.ID, nil
		}
	}

	return "", fmt.Errorf("no workflow found with name %q in pipeline %s", workflowName, pipelineNumber)
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

	pipelineNumber, ok := metadata["pipeline_number"].(string)
	if !ok || strings.TrimSpace(pipelineNumber) == "" {
		return fmt.Errorf("ci_metadata.pipeline_number is required")
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
		PipelineNumber:  metadata["pipeline_number"].(string),
		ApprovalJobName: metadata["approval_job_name"].(string),
	}, nil
}
