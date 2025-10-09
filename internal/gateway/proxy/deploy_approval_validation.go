package proxy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"gopkg.in/yaml.v3"
)

type buildApprovalContext struct {
	approvalID int64
	gitCommit  string
	app        string
}

// validateBuildRequest validates build requests against active deploy approvals.
// This is only called for API token requests.
// It verifies that:
// 1. The git-sha in the request matches an approved deploy request
// 2. Stores the context for updating the approval after successful build
func (h *Handler) validateBuildRequest(r *http.Request, bodyBytes []byte, tokenID int64) error {

	// Parse git-sha from request body (form-encoded: git-sha=abc123&url=...)
	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return fmt.Errorf("invalid build request body")
	}

	gitSHA := strings.TrimSpace(vals.Get("git-sha"))
	if gitSHA == "" {
		// No git-sha in request, skip validation
		return nil
	}

	// Check if there's an active approved deployment for this token and git commit
	approval, err := h.database.ActiveDeployApprovalRequestByTokenAndCommit(tokenID, gitSHA)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			return fmt.Errorf("deployment approval required for git commit %s", gitSHA)
		}
		return fmt.Errorf("failed to check deploy approval: %w", err)
	}

	if approval == nil {
		return fmt.Errorf("deployment approval required for git commit %s", gitSHA)
	}

	// Store approval in context so we can update it after successful build
	app := extractAppFromPath(r.URL.Path)
	ctx := context.WithValue(r.Context(), "buildApproval", &buildApprovalContext{
		approvalID: approval.ID,
		gitCommit:  gitSHA,
		app:        app,
	})
	*r = *r.WithContext(ctx)

	return nil
}

// updateBuildApprovalTracking updates the deploy approval request with build_id and release_id
// after a successful build creation
func (h *Handler) updateBuildApprovalTracking(r *http.Request, buildID, releaseID string) {
	ctx, ok := r.Context().Value("buildApproval").(*buildApprovalContext)
	if !ok || ctx == nil {
		return
	}

	if buildID == "" || releaseID == "" {
		return
	}

	err := h.database.UpdateDeployApprovalRequestBuild(ctx.approvalID, buildID, releaseID)
	if err != nil {
		// Log error but don't fail the request - build already succeeded
		fmt.Printf("WARNING: failed to update deploy approval tracking: %v\n", err)
	}
}

const (
	maxExtractedSize = 100 * 1024 * 1024 // 100MB max total extracted size
	maxFiles         = 10000             // Max number of files to extract
	maxFileSize      = 10 * 1024 * 1024  // 10MB max per file
	maxManifestSize  = 1 * 1024 * 1024   // 1MB max for convox.yml
)

type convoxManifest struct {
	Services map[string]convoxService `yaml:"services"`
}

type convoxService struct {
	Image string `yaml:"image"`
	Build string `yaml:"build"`
}

// validateObjectUpload validates object uploads (build tarballs) against deploy approvals.
// It extracts the tarball, finds convox.yml, and validates image tags match the approved git commit.
func (h *Handler) validateObjectUpload(r *http.Request, bodyBytes []byte, tokenID int64) error {
	// Get app name from path
	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		return nil // Can't validate without app name
	}

	// Get app image tag patterns from settings
	patterns, err := h.database.GetAppImageTagPatterns()
	if err != nil {
		return fmt.Errorf("failed to get app image tag patterns: %w", err)
	}

	// Check if this app requires manifest validation
	patternTemplate, hasPattern := patterns[app]
	if !hasPattern {
		// App not configured for manifest validation, skip
		return nil
	}

	// Get approval for this token
	approval, err := h.getActiveApprovalForToken(tokenID)
	if err != nil {
		return err
	}
	if approval == nil {
		return fmt.Errorf("deployment approval required for app %s", app)
	}

	// Extract tarball and validate manifest
	manifest, err := extractAndParseManifest(bodyBytes)
	if err != nil {
		return fmt.Errorf("failed to extract manifest: %w", err)
	}

	// Replace {{GIT_COMMIT}} with actual commit hash from approval
	pattern := strings.ReplaceAll(patternTemplate, "{{GIT_COMMIT}}", approval.GitCommitHash)

	// Validate all service images match the pattern
	if err := validateServiceImages(manifest, pattern); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	return nil
}

// getActiveApprovalForToken gets the active approval for an API token
func (h *Handler) getActiveApprovalForToken(tokenID int64) (*db.DeployApprovalRequest, error) {
	// Check if there's an active approved deployment for this token
	// We don't know the git commit yet, so we need to list all active approvals for this token
	reqs, err := h.database.ListDeployApprovalRequests(db.DeployApprovalRequestListOptions{
		Status:   db.DeployApprovalRequestStatusApproved,
		TokenID:  tokenID,
		OnlyOpen: true,
		Limit:    1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to check deploy approvals: %w", err)
	}

	if len(reqs) == 0 {
		return nil, nil
	}

	// Return the most recent active approval
	return reqs[0], nil
}

// extractAndParseManifest safely extracts a gzipped tarball and parses convox.yml
func extractAndParseManifest(tarballData []byte) (*convoxManifest, error) {
	// Create temp directory for extraction
	tmpDir, err := os.MkdirTemp("", "convox-manifest-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract tarball with safety limits
	manifestPath, err := extractTarballSafely(bytes.NewReader(tarballData), tmpDir)
	if err != nil {
		return nil, err
	}

	if manifestPath == "" {
		return nil, fmt.Errorf("convox.yml not found in tarball")
	}

	// Read and parse manifest
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	if len(manifestData) > maxManifestSize {
		return nil, fmt.Errorf("manifest too large: %d bytes (max %d)", len(manifestData), maxManifestSize)
	}

	var manifest convoxManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	return &manifest, nil
}

// extractTarballSafely extracts a gzipped tarball with safety limits and returns path to convox.yml
func extractTarballSafely(r io.Reader, destDir string) (string, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	var totalSize int64
	var fileCount int
	var manifestPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar header: %w", err)
		}

		// Check file count limit
		fileCount++
		if fileCount > maxFiles {
			return "", fmt.Errorf("too many files in tarball (max %d)", maxFiles)
		}

		// Validate file path to prevent directory traversal
		if err := validateTarPath(header.Name); err != nil {
			return "", err
		}

		targetPath := filepath.Join(destDir, header.Name)

		// Check if this is convox.yml
		baseName := filepath.Base(header.Name)
		if baseName == "convox.yml" && manifestPath == "" {
			manifestPath = targetPath
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return "", fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Check individual file size
			if header.Size > maxFileSize {
				return "", fmt.Errorf("file %s too large: %d bytes (max %d)", header.Name, header.Size, maxFileSize)
			}

			// Check total extracted size
			totalSize += header.Size
			if totalSize > maxExtractedSize {
				return "", fmt.Errorf("total extracted size exceeds limit: %d bytes (max %d)", totalSize, maxExtractedSize)
			}

			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return "", fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Extract file
			outFile, err := os.Create(targetPath)
			if err != nil {
				return "", fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.CopyN(outFile, tr, header.Size); err != nil {
				outFile.Close()
				return "", fmt.Errorf("failed to write file: %w", err)
			}
			outFile.Close()

		default:
			// Skip other file types (symlinks, etc.)
			continue
		}
	}

	return manifestPath, nil
}

// validateTarPath validates that a tar entry path is safe (no directory traversal)
func validateTarPath(path string) error {
	// Clean the path
	cleanPath := filepath.Clean(path)

	// Check for absolute paths
	if filepath.IsAbs(cleanPath) {
		return fmt.Errorf("absolute paths not allowed: %s", path)
	}

	// Check for path traversal attempts
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	return nil
}

// validateServiceImages validates that all service images match the required pattern
func validateServiceImages(manifest *convoxManifest, pattern string) error {
	if manifest == nil || len(manifest.Services) == 0 {
		return fmt.Errorf("no services defined in manifest")
	}

	// Compile regex pattern
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid image tag pattern: %w", err)
	}

	for serviceName, service := range manifest.Services {
		// Skip services that use build instead of image
		if service.Build != "" && service.Image == "" {
			continue
		}

		if service.Image == "" {
			return fmt.Errorf("service %s has no image defined", serviceName)
		}

		// Validate image tag matches pattern
		if !re.MatchString(service.Image) {
			return fmt.Errorf("service %s image %q does not match required pattern %q", serviceName, service.Image, pattern)
		}
	}

	return nil
}
