package proxy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	maxExtractedSize = 100 * 1024 * 1024 // 100MB max total extracted size
	maxFiles         = 10000             // Max number of files to extract
	maxFileSize      = 10 * 1024 * 1024  // 10MB max per file
	maxManifestSize  = 1 * 1024 * 1024   // 1MB max for convox.yml
	maxTarballSize   = 500 * 1024 * 1024 // 500MB max tarball download
)

type convoxManifest struct {
	Services map[string]convoxService `yaml:"services"`
}

type convoxService struct {
	Image string      `yaml:"image"`
	Build interface{} `yaml:"build"` // Can be string or map, we don't validate it
	// Ignore all other fields
}

// validateBuildManifest fetches the tarball from the Convox API and validates the manifest
func (h *Handler) validateBuildManifest(
	ctx context.Context,
	app, objectURL, manifestPath string,
	servicePatterns map[string]string,
	gitCommit string,
) error {
	// Extract the object key from the URL (e.g., "object://myapp/tmp/file.tgz" -> "tmp/file.tgz")
	// The object URL format is: object://app/key
	if !strings.HasPrefix(objectURL, "object://") {
		return fmt.Errorf("invalid object URL format: %s", objectURL)
	}

	parts := strings.SplitN(strings.TrimPrefix(objectURL, "object://"), "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid object URL format: %s", objectURL)
	}
	objectKey := parts[1]

	// Fetch the tarball from Convox API
	tarballData, err := h.fetchObject(ctx, app, objectKey)
	if err != nil {
		return fmt.Errorf("failed to fetch tarball: %w", err)
	}

	// Extract and parse the manifest
	manifest, err := extractAndParseManifestFromPath(tarballData, manifestPath)
	if err != nil {
		return fmt.Errorf("failed to extract manifest: %w", err)
	}

	// Replace {{GIT_COMMIT}} in all patterns with actual commit hash
	patterns := make(map[string]string)
	for service, patternTemplate := range servicePatterns {
		patterns[service] = strings.ReplaceAll(patternTemplate, "{{GIT_COMMIT}}", gitCommit)
	}

	// Validate all service images match their patterns
	return validateServiceImages(manifest, patterns)
}

// fetchObject fetches an object from the Convox API
func (h *Handler) fetchObject(ctx context.Context, app, key string) ([]byte, error) {
	// Get the rack config (there's only one per gateway instance)
	rack, exists := h.config.Racks["default"]
	if !exists {
		// Try local rack in dev mode
		rack, exists = h.config.Racks["local"]
		if !exists {
			return nil, fmt.Errorf("no rack configured")
		}
	}

	// Build URL to fetch object: GET /apps/{app}/objects/{key}
	fetchURL := fmt.Sprintf("%s/apps/%s/objects/%s", rack.URL, app, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add Convox API authentication
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s",
		base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))

	client, err := h.httpClient(ctx, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch object: %w", err)
	}
	//nolint:errcheck,gosec // G104: cleanup
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("object fetch failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read the tarball with size limit
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTarballSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read tarball: %w", err)
	}

	// Check if we hit the size limit
	if len(data) >= maxTarballSize {
		return nil, fmt.Errorf("tarball exceeds maximum size of %d bytes", maxTarballSize)
	}

	return data, nil
}

// extractAndParseManifestFromPath safely extracts a gzipped tarball and parses the specified manifest file
func extractAndParseManifestFromPath(tarballData []byte, manifestPath string) (*convoxManifest, error) {
	// Create temp directory for extraction
	tmpDir, err := os.MkdirTemp("", "convox-manifest-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck // cleanup

	// Extract tarball with safety limits, looking for the specified manifest file
	extractedManifestPath, err := extractTarballSafely(bytes.NewReader(tarballData), tmpDir, manifestPath)
	if err != nil {
		return nil, err
	}

	if extractedManifestPath == "" {
		return nil, fmt.Errorf("%s not found in tarball", manifestPath)
	}

	// Read and parse manifest
	//nolint:gosec // G304: Path validated by extraction process
	manifestData, err := os.ReadFile(extractedManifestPath)
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

// extractTarballSafely extracts a gzipped tarball with safety limits and returns path to the specified manifest file
func extractTarballSafely(r io.Reader, destDir string, manifestPath string) (string, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close() //nolint:errcheck // cleanup

	tr := tar.NewReader(gzr)

	var totalSize int64
	var fileCount int
	var foundManifestPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar header: %w", err)
		}

		if err := incrementAndCheckFileCount(&fileCount); err != nil {
			return "", err
		}
		if err := validateTarPath(header.Name); err != nil {
			return "", err
		}

		//nolint:gosec // G305: Path validated by validateTarPath()
		targetPath := filepath.Join(destDir, header.Name)
		foundManifestPath = updateFoundManifestPath(header.Name, manifestPath, foundManifestPath, targetPath)

		if err := handleTarEntry(tr, header, targetPath, &totalSize); err != nil {
			if err == errSkipEntry {
				continue
			}
			return "", err
		}
	}

	return foundManifestPath, nil
}

var errSkipEntry = fmt.Errorf("skip")

func incrementAndCheckFileCount(fileCount *int) error {
	*fileCount++
	if *fileCount > maxFiles {
		return fmt.Errorf("too many files in tarball (max %d)", maxFiles)
	}
	return nil
}

func updateFoundManifestPath(name, manifestPath, current, targetPath string) string {
	if name == manifestPath && current == "" {
		return targetPath
	}
	return current
}

func handleTarEntry(
	tr *tar.Reader,
	header *tar.Header,
	targetPath string,
	totalSize *int64,
) error {
	switch header.Typeflag {
	case tar.TypeDir:
		return extractTarDirectory(targetPath)
	case tar.TypeReg:
		newTotal, err := extractTarFile(tr, targetPath, header, *totalSize)
		if err != nil {
			return err
		}
		*totalSize = newTotal
		return nil
	default:
		return errSkipEntry
	}
}

// handleTarHeader processes a single tar header entry with safety checks and extraction logic.
// handleTarHeader was inlined into extractTarballSafely to avoid unnecessary complexity.

func extractTarDirectory(targetPath string) error {
	if err := os.MkdirAll(targetPath, 0o750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

func extractTarFile(tr *tar.Reader, targetPath string, header *tar.Header, currentSize int64) (int64, error) {
	// Check individual file size
	if header.Size > maxFileSize {
		return 0, fmt.Errorf("file %s too large: %d bytes (max %d)", header.Name, header.Size, maxFileSize)
	}

	// Check total extracted size
	newSize := currentSize + header.Size
	if newSize > maxExtractedSize {
		return 0, fmt.Errorf(
			"total extracted size exceeds limit: %d bytes (max %d)",
			newSize,
			maxExtractedSize,
		)
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return 0, fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Extract file
	//nolint:gosec // G304: targetPath validated by validateTarPath()
	outFile, err := os.Create(targetPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}

	if _, err := io.CopyN(outFile, tr, header.Size); err != nil {
		outFile.Close() //nolint:errcheck,gosec // G104: cleanup on error
		return 0, fmt.Errorf("failed to write file: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return 0, fmt.Errorf("failed to close file: %w", err)
	}

	return newSize, nil
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

// validateServiceImages validates that all service images match their required patterns
func validateServiceImages(manifest *convoxManifest, servicePatterns map[string]string) error {
	if manifest == nil || len(manifest.Services) == 0 {
		return fmt.Errorf("no services defined in manifest")
	}

	// Compile all patterns
	compiledPatterns := make(map[string]*regexp.Regexp)
	for service, pattern := range servicePatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid image pattern for service %s: %w", service, err)
		}
		compiledPatterns[service] = re
	}

	for serviceName, service := range manifest.Services {
		// Check if this service has a pattern configured
		pattern, hasPattern := compiledPatterns[serviceName]
		var patternString string

		if hasPattern {
			// Use service-specific pattern
			patternString = servicePatterns[serviceName]
		} else if wildcardPattern, hasWildcard := compiledPatterns["*"]; hasWildcard {
			// Use wildcard pattern as fallback (lowest precedence)
			pattern = wildcardPattern
			hasPattern = true
			patternString = servicePatterns["*"]
		}

		if !hasPattern {
			// No pattern for this service - skip validation
			continue
		}

		// When image pattern is configured for a service, it must use a pre-built image
		if service.Image == "" {
			return fmt.Errorf(
				"service %s must use a pre-built image (image pattern is configured for this service)",
				serviceName,
			)
		}

		// Validate image matches pattern
		if !pattern.MatchString(service.Image) {
			return fmt.Errorf(
				"service %s image %q does not match required pattern %q",
				serviceName,
				service.Image,
				patternString,
			)
		}
	}

	return nil
}
