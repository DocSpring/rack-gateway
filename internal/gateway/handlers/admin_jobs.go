package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/pagination"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// requireJobsAccess checks authentication, authorization, and jobs client availability
func (h *AdminHandler) requireJobsAccess(c *gin.Context, action rbac.Action) bool {
	if _, ok := requireAuth(c, h.rbac, rbac.ResourceJob, action); !ok {
		return false
	}

	if h.jobsClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "jobs system unavailable"})
		return false
	}

	return true
}

// parseJobID extracts and validates the job ID from the request path parameter.
// Returns the job ID and true if valid, or responds with an error and returns false.
func parseJobID(c *gin.Context) (int64, bool) {
	idStr := strings.TrimSpace(c.Param("id"))
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id is required"})
		return 0, false
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job id"})
		return 0, false
	}

	return id, true
}

// parseJobStateFilter converts a state string to River job state.
// Returns nil and false if the state is empty.
// Returns nil and false with an error response if the state is invalid.
func parseJobStateFilter(c *gin.Context, state string) (rivertype.JobState, bool) {
	if state == "" {
		return "", true
	}

	var riverState rivertype.JobState
	switch state {
	case "available":
		riverState = rivertype.JobStateAvailable
	case "canceled":
		riverState = rivertype.JobStateCancelled
	case "completed":
		riverState = rivertype.JobStateCompleted
	case "discarded":
		riverState = rivertype.JobStateDiscarded
	case "pending":
		riverState = rivertype.JobStatePending
	case "retryable":
		riverState = rivertype.JobStateRetryable
	case "running":
		riverState = rivertype.JobStateRunning
	case "scheduled":
		riverState = rivertype.JobStateScheduled
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state filter"})
		return "", false
	}
	return riverState, true
}

// parseJobListLimit parses and validates the limit query parameter.
func parseJobListLimit(limitStr string) int {
	const defaultLimit = 100
	const maxLimit = 1000

	if limitStr == "" {
		return defaultLimit
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(limitStr))
	if err != nil || parsed <= 0 {
		return defaultLimit
	}

	if parsed > maxLimit {
		return maxLimit
	}

	return parsed
}

// ListJobs godoc
// @Summary List background jobs
// @Description Returns a list of background jobs with optional filtering
// @Tags Jobs
// @Produce json
// @Param queue query string false "Filter by queue name"
// @Param state query string false "Filter by job state"
// @Param kind query string false "Filter by job kind"
// @Param limit query integer false "Maximum number of results (default: 100, max: 1000)"
// @Success 200 {object} JobListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /jobs [get]
func (h *AdminHandler) ListJobs(c *gin.Context) {
	if !h.requireJobsAccess(c, rbac.ActionList) {
		return
	}

	// Parse filters and cursor
	limit := parseJobListLimit(c.Query("limit"))
	queue := strings.TrimSpace(c.Query("queue"))
	stateStr := strings.TrimSpace(c.Query("state"))
	kind := strings.TrimSpace(c.Query("kind"))
	afterCursor := strings.TrimSpace(c.Query("after"))

	// Parse cursor
	cursorID, err := parseJobCursor(c, afterCursor)
	if err != nil {
		return
	}

	// Parse state filter
	var riverState *rivertype.JobState
	if stateStr != "" {
		parsed, ok := parseJobStateFilter(c, stateStr)
		if !ok {
			return
		}
		riverState = &parsed
	}

	// Fetch jobs
	jobs, err := h.fetchJobsWithFilters(c, cursorID, limit+1, riverState, kind, queue)
	if err != nil {
		return
	}

	// Build response
	h.respondJobList(c, jobs, limit, afterCursor != "")
}

func (h *AdminHandler) fetchJobsWithFilters(
	c *gin.Context,
	cursorID *int64,
	limit int,
	state *rivertype.JobState,
	kind, queue string,
) ([]*rivertype.JobRow, error) {
	if cursorID != nil {
		// Use custom SQL for cursor-based filtering
		return h.jobsClient.JobListWithCursor(c.Request.Context(), cursorID, limit, state, kind, queue)
	}

	// No cursor - use River's native API
	params := river.NewJobListParams().
		First(limit).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc)

	if queue != "" {
		params = params.Queues(queue)
	}
	if state != nil {
		params = params.States(*state)
	}
	if kind != "" {
		params = params.Kinds(kind)
	}

	result, err := h.jobsClient.JobList(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list jobs"})
		return nil, err
	}

	return result.Jobs, nil
}

func (h *AdminHandler) respondJobList(c *gin.Context, jobs []*rivertype.JobRow, limit int, hasBefore bool) {
	// Detect if there are more pages and trim to limit
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}

	// Convert to response format
	responses := make([]JobResponse, 0, len(jobs))
	for _, job := range jobs {
		responses = append(responses, toJobResponse(job))
	}

	// Build page info (no count for River - expensive)
	pageInfo := pagination.GetRiverPageInfo(jobs, limit, hasBefore)

	c.JSON(http.StatusOK, JobListResponse{
		Jobs:     responses,
		PageInfo: pageInfo,
	})
}

func parseJobCursor(c *gin.Context, afterCursor string) (*int64, error) {
	if afterCursor == "" {
		return nil, nil
	}

	cursor, err := pagination.ParseCursor(afterCursor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor"})
		return nil, err
	}

	if cursor == nil {
		return nil, nil
	}

	return &cursor.ID, nil
}

// GetJob godoc
// @Summary Get a background job
// @Description Returns details of a specific background job by ID
// @Tags Jobs
// @Produce json
// @Param id path integer true "Job ID"
// @Success 200 {object} JobResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /jobs/{id} [get]
func (h *AdminHandler) GetJob(c *gin.Context) {
	if !h.requireJobsAccess(c, rbac.ActionRead) {
		return
	}

	id, ok := parseJobID(c)
	if !ok {
		return
	}

	job, err := h.jobsClient.JobGet(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch job"})
		}
		return
	}

	c.JSON(http.StatusOK, toJobResponse(job))
}

// DeleteJob godoc
// @Summary Delete a background job
// @Description Cancels a background job by ID
// @Tags Jobs
// @Produce json
// @Param id path integer true "Job ID"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /jobs/{id} [delete]
func (h *AdminHandler) DeleteJob(c *gin.Context) {
	if !h.requireJobsAccess(c, rbac.ActionDelete) {
		return
	}

	id, ok := parseJobID(c)
	if !ok {
		return
	}

	_, err := h.jobsClient.JobCancel(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete job"})
		}
		return
	}

	c.Status(http.StatusNoContent)
}

// RetryJob godoc
// @Summary Retry a background job
// @Description Immediately retries a background job by ID
// @Tags Jobs
// @Produce json
// @Param id path integer true "Job ID"
// @Success 200 {object} JobResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /jobs/{id}/retry [post]
func (h *AdminHandler) RetryJob(c *gin.Context) {
	if !h.requireJobsAccess(c, rbac.ActionUpdate) {
		return
	}

	id, ok := parseJobID(c)
	if !ok {
		return
	}

	job, err := h.jobsClient.JobRetry(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retry job"})
		}
		return
	}

	c.JSON(http.StatusOK, toJobResponse(job))
}

func toJobResponse(job *rivertype.JobRow) JobResponse {
	var lastError string
	if len(job.Errors) > 0 {
		lastError = job.Errors[len(job.Errors)-1].Error
	}

	// Decode args from byte array to JSON
	// The args are stored as bytes in the database, we just pass them through as-is
	// and let JSON marshal handle the conversion
	var args json.RawMessage
	if len(job.EncodedArgs) > 0 {
		// River stores args as JSON bytes, we just return them as JSON
		args = job.EncodedArgs
	}

	return JobResponse{
		ID:          job.ID,
		State:       string(job.State),
		Queue:       job.Queue,
		Kind:        job.Kind,
		Args:        args,
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
		CreatedAt:   job.CreatedAt,
		ScheduledAt: job.ScheduledAt,
		AttemptedAt: job.AttemptedAt,
		FinalizedAt: job.FinalizedAt,
		Errors:      formatJobErrors(job.Errors),
		LastError:   lastError,
	}
}

func formatJobErrors(jobErrors []rivertype.AttemptError) []JobErrorResponse {
	result := make([]JobErrorResponse, 0, len(jobErrors))
	for _, e := range jobErrors {
		result = append(result, JobErrorResponse{
			Attempt: e.Attempt,
			Error:   e.Error,
			At:      e.At,
		})
	}
	return result
}

// JobListResponse contains a list of jobs with cursor pagination (no count for River)
type JobListResponse struct {
	Jobs     []JobResponse       `json:"jobs"`
	PageInfo pagination.PageInfo `json:"page_info"`
}

// JobResponse represents a background job with its execution details and errors.
type JobResponse struct {
	ID          int64              `json:"id"`
	State       string             `json:"state"`
	Queue       string             `json:"queue"`
	Kind        string             `json:"kind"`
	Args        json.RawMessage    `json:"args,omitempty"`
	Attempt     int                `json:"attempt"`
	MaxAttempts int                `json:"max_attempts"`
	CreatedAt   time.Time          `json:"created_at"`
	ScheduledAt time.Time          `json:"scheduled_at"`
	AttemptedAt *time.Time         `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time         `json:"finalized_at,omitempty"`
	Errors      []JobErrorResponse `json:"errors,omitempty"`
	LastError   string             `json:"last_error,omitempty"`
}

// JobErrorResponse represents an error that occurred during job execution.
type JobErrorResponse struct {
	Attempt int       `json:"attempt"`
	Error   string    `json:"error"`
	At      time.Time `json:"at"`
}
