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

	// Parse filters
	limit := parseJobListLimit(c.Query("limit"))
	queue := strings.TrimSpace(c.Query("queue"))
	stateStr := strings.TrimSpace(c.Query("state"))
	kind := strings.TrimSpace(c.Query("kind"))

	// Build River query params using builder pattern
	params := river.NewJobListParams().First(limit)

	// Add queue filter
	if queue != "" {
		params = params.Queues(queue)
	}

	// Add state filter
	if stateStr != "" {
		riverState, ok := parseJobStateFilter(c, stateStr)
		if !ok {
			return
		}
		params = params.States(riverState)
	}

	// Add kind filter
	if kind != "" {
		params = params.Kinds(kind)
	}

	// Fetch jobs from River
	result, err := h.jobsClient.JobList(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list jobs"})
		return
	}

	// Convert to response format
	responses := make([]JobResponse, 0, len(result.Jobs))
	for _, job := range result.Jobs {
		responses = append(responses, toJobResponse(job))
	}

	c.JSON(http.StatusOK, JobListResponse{
		Jobs:  responses,
		Count: len(responses),
		Limit: limit,
	})
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

	idStr := strings.TrimSpace(c.Param("id"))
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id is required"})
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job id"})
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

// JobListResponse contains a list of jobs with count and pagination metadata.
type JobListResponse struct {
	Jobs  []JobResponse `json:"jobs"`
	Count int           `json:"count"` // Number of jobs in this response (not total count across all pages)
	Limit int           `json:"limit"`
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
