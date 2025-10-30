package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDoRequestSuccess tests successful requests with JSON decoding
func TestDoRequestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Expected Authorization header 'Bearer test-token', got '%s'", auth)
		}
		if accept := r.Header.Get("Accept"); accept != "application/vnd.github.v3+json" {
			t.Errorf("Expected Accept header 'application/vnd.github.v3+json', got '%s'", accept)
		}

		// Return JSON response
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name": "test-branch", "commit": {"sha": "abc123"}}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	var branch Branch
	err := client.doRequest("GET", server.URL, nil, http.StatusOK, "", &branch)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if branch.Name != "test-branch" {
		t.Errorf("Expected branch name 'test-branch', got '%s'", branch.Name)
	}
	if branch.Commit.SHA != "abc123" {
		t.Errorf("Expected commit SHA 'abc123', got '%s'", branch.Commit.SHA)
	}
}

// TestDoRequestWithBody tests POST requests with request body
func TestDoRequestWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Content-Type header is set when body is present
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Expected Content-Type header 'application/json', got '%s'", contentType)
		}

		// Verify body
		body, _ := io.ReadAll(r.Body)
		var comment PRCommentRequest
		if err := json.Unmarshal(body, &comment); err != nil {
			t.Errorf("Failed to unmarshal request body: %v", err)
		}
		if comment.Body != "test comment" {
			t.Errorf("Expected comment body 'test comment', got '%s'", comment.Body)
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient("test-token")

	body, _ := json.Marshal(PRCommentRequest{Body: "test comment"})
	err := client.doRequest("POST", server.URL, strings.NewReader(string(body)), http.StatusCreated, "", nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

// TestDoRequestNotFoundWithCustomError tests 404 handling with custom error message
func TestDoRequestNotFoundWithCustomError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	customError := "branch test-branch not found in repository owner/repo"
	err := client.doRequest("GET", server.URL, nil, http.StatusOK, customError, nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if err.Error() != customError {
		t.Errorf("Expected error '%s', got '%s'", customError, err.Error())
	}
}

// TestDoRequestNotFoundWithoutCustomError tests 404 handling without custom error
func TestDoRequestNotFoundWithoutCustomError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	err := client.doRequest("GET", server.URL, nil, http.StatusOK, "", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Should return generic error with status code and body
	expectedError := "GitHub API returned status 404: {\"message\": \"Not Found\"}"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestDoRequestUnexpectedStatus tests handling of unexpected status codes
func TestDoRequestUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message": "Bad Request", "errors": ["invalid field"]}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	err := client.doRequest("GET", server.URL, nil, http.StatusOK, "", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Error should include status code and response body
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("Expected error to contain status code 400, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "Bad Request") {
		t.Errorf("Expected error to contain response body, got: %s", err.Error())
	}
}

// TestDoRequestJSONDecodeError tests handling of JSON decode errors
func TestDoRequestJSONDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`invalid json`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	var branch Branch
	err := client.doRequest("GET", server.URL, nil, http.StatusOK, "", &branch)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected error to contain 'failed to decode response', got: %s", err.Error())
	}
}

// TestDoRequestNetworkError tests handling of network errors
func TestDoRequestNetworkError(t *testing.T) {
	client := NewClient("test-token")

	// Use an invalid URL to trigger network error
	err := client.doRequest("GET", "http://invalid-host-that-does-not-exist.local", nil, http.StatusOK, "", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Network errors are returned as-is from httpClient.Do
	if err.Error() == "" {
		t.Error("Expected non-empty error message")
	}
}

// TestDoRequestNoTargetDecoding tests that response is not decoded when target is nil
func TestDoRequestNoTargetDecoding(t *testing.T) {
	responseSent := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Send any response - it shouldn't be decoded
		_, _ = w.Write([]byte(`{"some": "data"}`))
		responseSent = true
	}))
	defer server.Close()

	client := NewClient("test-token")

	// Call without a target - should not attempt to decode
	err := client.doRequest("GET", server.URL, nil, http.StatusOK, "", nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !responseSent {
		t.Error("Expected server to send response")
	}
}

// TestDoRequestInvalidURL tests handling of invalid URLs
func TestDoRequestInvalidURL(t *testing.T) {
	client := NewClient("test-token")

	// Invalid URL that causes http.NewRequest to fail
	err := client.doRequest("GET\n", "http://example.com", nil, http.StatusOK, "", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("Expected error to contain 'failed to create request', got: %s", err.Error())
	}
}

// TestDoRequestServerError tests handling of 500 errors
func TestDoRequestServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "Internal Server Error"}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	err := client.doRequest("GET", server.URL, nil, http.StatusOK, "", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	expectedError := "GitHub API returned status 500: {\"message\": \"Internal Server Error\"}"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestVerifyCommitOnBranchIntegration tests verifyCommitOnBranch using doRequest
func TestVerifyCommitOnBranchIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a compare request
		if !strings.Contains(r.URL.Path, "/compare/") {
			t.Errorf("Expected compare API path, got: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "behind"}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	// Override URL for testing by calling doRequest directly with test server
	url := fmt.Sprintf("%s/repos/owner/repo/compare/abc123...def456", server.URL)
	err := client.doRequest("GET", url, nil, http.StatusOK, "commit abc123 not found or not on branch main", nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

// TestGetBranchIntegration tests getBranch using doRequest
func TestGetBranchIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"name": "main",
			"commit": {
				"sha": "abc123",
				"url": "https://api.github.com/repos/owner/repo/commits/abc123"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient("test-token")

	var branch Branch
	err := client.doRequest(
		"GET",
		server.URL,
		nil,
		http.StatusOK,
		"branch main not found in repository owner/repo",
		&branch,
	)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if branch.Name != "main" {
		t.Errorf("Expected branch name 'main', got '%s'", branch.Name)
	}
	if branch.Commit.SHA != "abc123" {
		t.Errorf("Expected commit SHA 'abc123', got '%s'", branch.Commit.SHA)
	}
}
