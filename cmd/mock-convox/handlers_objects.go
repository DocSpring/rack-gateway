package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

var errInvalidObjectPath = errors.New("invalid object path")

// validatePath ensures path doesn't contain traversal attempts
func validatePath(path string) error {
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return fmt.Errorf("path traversal detected")
	}
	if filepath.IsAbs(clean) {
		return fmt.Errorf("absolute paths not allowed")
	}
	return nil
}

// validateSafePath ensures the constructed path stays within baseDir
func validateSafePath(baseDir, constructedPath string) error {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve base path: %w", err)
	}
	absPath, err := filepath.Abs(constructedPath)
	if err != nil {
		return fmt.Errorf("failed to resolve target path: %w", err)
	}

	relPath, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return fmt.Errorf("failed to resolve relative path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes base directory")
	}
	return nil
}

func ensureObjectUploadDir(app string) (string, error) {
	appDir := filepath.Join(objectStorageDir, app)
	if err := validateSafePath(objectStorageDir, appDir); err != nil {
		return "", fmt.Errorf("%w: %v", errInvalidObjectPath, err)
	}
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create app directory: %w", err)
	}

	objectDir := filepath.Join(appDir, "tmp")
	if err := validateSafePath(objectStorageDir, objectDir); err != nil {
		return "", fmt.Errorf("%w: %v", errInvalidObjectPath, err)
	}
	if err := os.MkdirAll(objectDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create tmp directory: %w", err)
	}

	return objectDir, nil
}

func openObjectFileForUpload(objectDir, name string) (*os.File, string, error) {
	objectPath := filepath.Join(objectDir, name)
	if err := validateSafePath(objectDir, objectPath); err != nil {
		return nil, "", fmt.Errorf("%w: %v", errInvalidObjectPath, err)
	}

	objectRoot, err := os.OpenRoot(objectDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open object root: %w", err)
	}

	file, err := objectRoot.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	closeErr := objectRoot.Close()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create object file: %w", err)
	}
	if closeErr != nil {
		if fileCloseErr := file.Close(); fileCloseErr != nil {
			mclog.Errorf("failed to close object file after root close error: %v", fileCloseErr)
		}
		return nil, "", fmt.Errorf("failed to close object root: %w", closeErr)
	}

	return file, objectPath, nil
}

func uploadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	name := vars["name"]

	if err := validatePath(app); err != nil {
		http.Error(w, "invalid app name", http.StatusBadRequest)
		return
	}
	if err := validatePath(name); err != nil {
		http.Error(w, "invalid object name", http.StatusBadRequest)
		return
	}

	objectDir, err := ensureObjectUploadDir(app)
	if err != nil {
		if errors.Is(err, errInvalidObjectPath) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		mclog.Errorf("failed to prepare upload directory: %v", err)
		http.Error(w, "failed to create storage directory", http.StatusInternalServerError)
		return
	}

	f, objectPath, err := openObjectFileForUpload(objectDir, name)
	if err != nil {
		if errors.Is(err, errInvalidObjectPath) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		mclog.Errorf("failed to create object file: %v", err)
		http.Error(w, "failed to save upload", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			mclog.Errorf("failed to close object file: %v", err)
		}
	}()

	written, err := io.Copy(f, r.Body)
	if err != nil {
		mclog.Errorf("failed to write upload: %v", err)
		http.Error(w, "failed to save upload", http.StatusInternalServerError)
		return
	}
	if err := r.Body.Close(); err != nil {
		mclog.Warnf("failed to close upload body: %v", err)
	}

	mclog.DebugTopicf(mclog.TopicAppObjects, "saved object to %s (%d bytes)", objectPath, written)

	objectURL := fmt.Sprintf("object://%s/tmp/%s", app, name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"Url": objectURL})
}

func downloadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	key := vars["key"]

	if err := validatePath(app); err != nil {
		http.Error(w, "invalid app name", http.StatusBadRequest)
		return
	}
	if err := validatePath(key); err != nil {
		http.Error(w, "invalid object key", http.StatusBadRequest)
		return
	}

	objectPath := filepath.Join(objectStorageDir, app, key)
	if err := validateSafePath(objectStorageDir, objectPath); err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	mclog.DebugTopicf(mclog.TopicAppObjects, "fetching object from %s", objectPath)

	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		mclog.Warnf("object not found: %s", objectPath)
		http.Error(w, "object not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, objectPath)
}
