package circleci

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	req, err := http.NewRequest(method, url, http.NoBody)
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
	defer func() { _ = resp.Body.Close() }()

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

// ApprovalTarget is a hold job PrepareApproval verified, ready to approve.
type ApprovalTarget struct {
	// WorkflowID and JobID name the hold job: the requested workflow's, or its newest rerun's.
	WorkflowID string
	JobID      string
	// PipelineID and PipelineNumber are the verified pipeline's.
	PipelineID     string
	PipelineNumber string
}

// PrepareApproval finds the hold job named jobName in a CircleCI workflow, after checking that the workflow's
// pipeline is the one the request named and builds the approved revision of the expected repository; otherwise it
// returns ErrPipelineMismatch. If the workflow was rerun, the target is the hold job in the newest rerun (see
// approvalTarget). Nothing is approved until Approve is called with the target.
func (c *Client) PrepareApproval(workflowID, jobName string, expect ApprovalExpectation) (*ApprovalTarget, error) {
	if err := ValidateWorkflowID(workflowID); err != nil {
		return nil, err
	}
	if jobName == "" {
		return nil, fmt.Errorf("job_name is required")
	}

	original, err := c.getWorkflowDetails(workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow details: %w", err)
	}

	pipeline, err := c.getPipeline(original.PipelineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline: %w", err)
	}
	if err := verifyPipeline(pipeline, expect); err != nil {
		return nil, err
	}

	targetWorkflowID, jobID, err := c.approvalTarget(original, jobName)
	if err != nil {
		return nil, err
	}
	return &ApprovalTarget{
		WorkflowID:     targetWorkflowID,
		JobID:          jobID,
		PipelineID:     pipeline.ID,
		PipelineNumber: strconv.FormatInt(pipeline.Number, 10),
	}, nil
}

// Approve approves the hold job PrepareApproval found.
func (c *Client) Approve(target *ApprovalTarget) error {
	url := fmt.Sprintf("%s/workflow/%s/approve/%s", c.BaseURL, target.WorkflowID, target.JobID)
	_, err := c.doRequest(http.MethodPost, url)
	return err
}

// approvalTarget returns the workflow and hold job to approve. While the original workflow's hold job is on hold,
// that's the one: CircleCI's pipeline workflow listing sometimes leaves a pipeline's workflows out for a while, so
// the usual case doesn't depend on it. Otherwise the listing is checked for a rerun (a newer workflow with the same
// name), and without one the original's hold job is approved anyway, leaving CircleCI to say why it can't be (for
// example, because it's already approved).
func (c *Client) approvalTarget(original *workflowDetails, jobName string) (workflowID, jobID string, err error) {
	job, err := c.findApprovalJob(original.ID, jobName)
	if err != nil {
		return "", "", err
	}
	if job.Status == jobStatusOnHold {
		return original.ID, job.ID, nil
	}

	rerunID, err := c.findRerun(original)
	if err != nil {
		return "", "", fmt.Errorf("failed to look for a rerun of workflow %s: %w", original.ID, err)
	}
	if rerunID == "" {
		return original.ID, job.ID, nil
	}
	rerunJob, err := c.findApprovalJob(rerunID, jobName)
	if err != nil {
		return "", "", err
	}
	return rerunID, rerunJob.ID, nil
}

// jobStatusOnHold is the status of an approval job that is waiting to be approved.
const jobStatusOnHold = "on_hold"

type workflowJob struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// findApprovalJob returns the approval job named jobName in a workflow.
func (c *Client) findApprovalJob(workflowID, jobName string) (*workflowJob, error) {
	url := fmt.Sprintf("%s/workflow/%s/job", c.BaseURL, workflowID)
	body, err := c.doRequest(http.MethodGet, url)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow jobs: %w", err)
	}

	var result struct {
		Items []workflowJob `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse workflow jobs: %w", err)
	}

	for i := range result.Items {
		if result.Items[i].Name == jobName && result.Items[i].Type == "approval" {
			return &result.Items[i], nil
		}
	}
	return nil, fmt.Errorf("approval job %q not found in workflow %s", jobName, workflowID)
}

type workflowDetails struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PipelineID string `json:"pipeline_id"`
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

// findRerun returns the newest workflow in the original's pipeline that has its name and was created after it, or
// "" when there is none.
func (c *Client) findRerun(original *workflowDetails) (string, error) {
	originalCreated, err := time.Parse(time.RFC3339, original.CreatedAt)
	if err != nil {
		return "", fmt.Errorf("failed to parse the workflow's created_at %q: %w", original.CreatedAt, err)
	}

	url := fmt.Sprintf("%s/pipeline/%s/workflow", c.BaseURL, original.PipelineID)
	body, err := c.doRequest(http.MethodGet, url)
	if err != nil {
		return "", err
	}

	var result struct {
		Items []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse pipeline workflows: %w", err)
	}

	rerunID, newest := "", originalCreated
	for _, wf := range result.Items {
		created, err := time.Parse(time.RFC3339, wf.CreatedAt)
		if wf.Name != original.Name || wf.ID == original.ID || err != nil || !created.After(newest) {
			continue
		}
		rerunID, newest = wf.ID, created
	}
	return rerunID, nil
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
	if err := ValidateWorkflowID(workflowID); err != nil {
		return fmt.Errorf("ci_metadata.workflow_id: %w", err)
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
