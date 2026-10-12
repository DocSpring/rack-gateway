package proxy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxExtractedSize = 100 * 1024 * 1024 // 100MB max total size of a tarball's files, uncompressed
	maxFiles         = 10000             // Max number of entries in a tarball
	maxFileSize      = 10 * 1024 * 1024  // 10MB max per file
	maxTarballSize   = 500 * 1024 * 1024 // 500MB max tarball read

	// maxManifestSize bounds convox.yml. A real one is a few kilobytes. The YAML parser builds a node for every
	// scalar before anything is checked (a couple of hundred bytes each, even for keys the gateway ignores), so a
	// megabyte of "[1,1,1,...]" would hold around 100MB of nodes, and this caps that at about 13MB.
	maxManifestSize = 128 * 1024
)

// validateBuildManifest fetches the build's archive from the rack and checks its manifest's images against the
// policy twice: as written (parseManifest, which also refuses what the rack's parser could read differently), and as
// the rack will load it, with the app's env and the build's arguments substituted (loadManifestAsRack). A refusal is
// a 403; a failure to read from the rack a 502.
func (h *Handler) validateBuildManifest(ctx context.Context, app string, vals url.Values, policy imagePolicy) error {
	objectURL := vals.Get("url")
	if objectURL == "" {
		return refuse(http.StatusBadRequest, errors.New("build request missing object URL"))
	}
	objectKey, err := objectKeyForApp(objectURL, app)
	if err != nil {
		return refuse(http.StatusBadRequest, err)
	}

	tarball, err := h.fetchObject(ctx, app, objectKey)
	if err != nil {
		return refuse(http.StatusBadGateway, fmt.Errorf("failed to fetch tarball: %w", err))
	}
	defer func() { _ = tarball.Close() }()

	data, err := readArchiveManifest(tarball, manifestPathFrom(vals), maxTarballSize)
	if err != nil {
		return refuseManifest(fmt.Errorf("failed to extract manifest: %w", err))
	}
	manifest, err := parseManifest(data)
	if err != nil {
		return refuseManifest(err)
	}
	if err := policy.validate(manifest); err != nil {
		return refuseManifest(err)
	}

	appEnv, err := h.fetchLatestReleaseEnv(ctx, app)
	if err != nil {
		return refuse(http.StatusBadGateway, fmt.Errorf("failed to read the app's env to check the manifest: %w", err))
	}
	asRack, err := loadManifestAsRack(data, buildManifestEnv(appEnv, vals.Get("build-args")))
	if err != nil {
		return refuseManifest(err)
	}
	if err := policy.validate(asRack); err != nil {
		return refuseManifest(fmt.Errorf("with the app's env and build arguments substituted, %w", err))
	}
	return nil
}

// refuseManifest refuses a build whose manifest failed a check.
func refuseManifest(err error) error {
	return refuse(http.StatusForbidden, fmt.Errorf("manifest validation failed: %w", err))
}

// objectKeyForApp returns the key of the app's object that a build's url names. The rack builds from the app and
// key in the parsed URL (pkg/build prepareSource: u.Host and u.Path), not from the app being built, so only a URL
// of exactly the form object://<app>/<key>, naming this app, is accepted. Another app, a port, a query, a fragment
// or an escape would have the rack build a different archive from the one checked here.
func objectKeyForApp(objectURL, app string) (string, error) {
	u, err := url.Parse(objectURL)
	if err != nil || u.Scheme != "object" || u.Host != app {
		return "", fmt.Errorf("build url must name an object of app %s: %s", app, objectURL)
	}
	key := strings.TrimPrefix(u.Path, "/")
	if key == "" || "object://"+app+"/"+key != objectURL {
		return "", fmt.Errorf("invalid object URL format: %s", objectURL)
	}
	return key, nil
}

// fetchObject opens an object on the Convox API, for the caller to stream and close.
func (h *Handler) fetchObject(ctx context.Context, app, key string) (io.ReadCloser, error) {
	// Get the rack config (there's only one per gateway instance)
	rack, exists := h.config.Racks["default"]
	if !exists {
		// Try local rack in dev mode
		rack, exists = h.config.Racks["local"]
		if !exists {
			return nil, fmt.Errorf("no rack configured")
		}
	}

	fetchURL, err := buildObjectFetchURL(rack.URL, app, key)
	if err != nil {
		return nil, err
	}

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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("object fetch failed with status %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

func buildObjectFetchURL(rackURL, app, key string) (string, error) {
	safeApp, err := sanitizeObjectFetchComponent(app)
	if err != nil {
		return "", fmt.Errorf("invalid app path component: %w", err)
	}

	safeKey, err := sanitizeObjectFetchPath(key)
	if err != nil {
		return "", fmt.Errorf("invalid object key path: %w", err)
	}

	baseURL, err := url.Parse(rackURL)
	if err != nil {
		return "", fmt.Errorf("invalid rack URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return "", fmt.Errorf("invalid rack URL: missing scheme or host")
	}

	fetchURL := *baseURL
	fetchURL.RawQuery = ""
	fetchURL.Fragment = ""
	fetchURL.Path = path.Join("/", baseURL.Path, "apps", safeApp, "objects", safeKey)

	if fetchURL.Host != baseURL.Host {
		return "", fmt.Errorf("fetch URL does not match configured rack host")
	}

	return fetchURL.String(), nil
}

func sanitizeObjectFetchComponent(value string) (string, error) {
	safeValue := strings.TrimSpace(value)
	if safeValue == "" {
		return "", fmt.Errorf("value is empty")
	}
	if strings.HasPrefix(safeValue, "/") || strings.Contains(safeValue, "/") || strings.Contains(safeValue, "\\") {
		return "", fmt.Errorf("value contains invalid separators")
	}

	cleanValue := path.Clean(safeValue)
	if cleanValue == "." || cleanValue == ".." || strings.HasPrefix(cleanValue, "..") {
		return "", fmt.Errorf("value escapes object namespace")
	}

	return cleanValue, nil
}

func sanitizeObjectFetchPath(value string) (string, error) {
	safeValue := strings.TrimSpace(value)
	if safeValue == "" {
		return "", fmt.Errorf("value is empty")
	}
	if strings.HasPrefix(safeValue, "/") || strings.Contains(safeValue, "\\") {
		return "", fmt.Errorf("value contains invalid separators")
	}

	segments := strings.Split(safeValue, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("value contains invalid path segment")
		}
	}

	cleanValue := path.Clean(safeValue)
	if cleanValue == "." || cleanValue == ".." || strings.HasPrefix(cleanValue, "../") {
		return "", fmt.Errorf("value escapes object namespace")
	}

	return cleanValue, nil
}

// readArchiveManifest returns the manifest at manifestPath in a gzipped tarball, reading the archive as it streams
// in and holding no more than the manifest in memory. An archive that reaches limit is refused even when the scan
// ended cleanly: the limit can fall exactly between two gzip members, where gzip and tar both see a normal end, and
// the rack would still extract the rest (which could hold another manifest).
func readArchiveManifest(r io.Reader, manifestPath string, limit int64) ([]byte, error) {
	limited := &io.LimitedReader{R: r, N: limit}
	manifestData, err := readManifestFromTarball(limited, manifestPath)
	if limited.N <= 0 {
		return nil, fmt.Errorf("tarball exceeds maximum size of %d bytes", limit)
	}
	if err != nil {
		return nil, err
	}
	return manifestData, nil
}

// readManifestFromTarball returns the contents of the manifest at manifestPath in a gzipped tarball. It reads the
// whole archive rather than stopping at the manifest: the rack extracts every entry in order, writing each file over
// any earlier one at the same path, so a second manifest entry would change the manifest the rack builds from after
// this one was checked. An archive with more than one is refused.
func readManifestFromTarball(r io.Reader, manifestPath string) ([]byte, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}

	tr := tar.NewReader(gzr)
	scan := manifestScan{path: filepath.Clean(manifestPath)}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}
		if err := scan.visit(tr, header); err != nil {
			return nil, err
		}
	}

	if !scan.found {
		return nil, fmt.Errorf("%s not found in tarball", manifestPath)
	}
	return scan.manifest, nil
}

// manifestScan is one pass over a build tarball, looking for its manifest.
type manifestScan struct {
	// path is the manifest's path, cleaned: the rack joins entry names onto its build directory, so
	// "./convox.yml" is convox.yml.
	path      string
	files     int
	totalSize int64
	found     bool
	manifest  []byte
}

// visit checks a tarball entry against the archive limits, and reads it if it's the manifest.
func (s *manifestScan) visit(entry io.Reader, header *tar.Header) error {
	if err := s.checkLimits(header); err != nil {
		return err
	}
	if filepath.Clean(header.Name) != s.path {
		return nil
	}
	if s.found {
		return fmt.Errorf("tarball contains %s more than once", s.path)
	}
	// The rack only extracts regular files, so a link or directory here isn't the manifest it would build from.
	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("%s in tarball is not a regular file", s.path)
	}
	if header.Size > maxManifestSize {
		return fmt.Errorf("manifest too large: %d bytes (max %d)", header.Size, maxManifestSize)
	}

	data, err := io.ReadAll(entry)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}
	s.found, s.manifest = true, data
	return nil
}

// checkLimits bounds the work of reading through an untrusted tarball: how many entries it has, how large its files
// are, and that every path stays inside the archive.
func (s *manifestScan) checkLimits(header *tar.Header) error {
	s.files++
	if s.files > maxFiles {
		return fmt.Errorf("too many files in tarball (max %d)", maxFiles)
	}
	if err := validateTarPath(header.Name); err != nil {
		return err
	}
	if header.Typeflag != tar.TypeReg {
		return nil
	}

	if header.Size > maxFileSize {
		return fmt.Errorf("file %s too large: %d bytes (max %d)", header.Name, header.Size, maxFileSize)
	}
	s.totalSize += header.Size
	if s.totalSize > maxExtractedSize {
		return fmt.Errorf(
			"total extracted size exceeds limit: %d bytes (max %d)",
			s.totalSize,
			maxExtractedSize,
		)
	}
	return nil
}

// validateTarPath validates that a tar entry path is safe (no directory traversal)
func validateTarPath(entryPath string) error {
	// Clean the path
	cleanPath := filepath.Clean(entryPath)

	// Check for absolute paths
	if filepath.IsAbs(cleanPath) {
		return fmt.Errorf("absolute paths not allowed: %s", entryPath)
	}

	// Check for path traversal attempts
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed: %s", entryPath)
	}

	return nil
}
