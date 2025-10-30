package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gorilla/mux"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

func uploadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	name := vars["name"]

	appDir := fmt.Sprintf("%s/%s", objectStorageDir, app)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		mclog.Errorf("failed to create app directory: %v", err)
		http.Error(w, "failed to create storage directory", http.StatusInternalServerError)
		return
	}

	objectPath := fmt.Sprintf("%s/tmp/%s", appDir, name)
	objectDir := fmt.Sprintf("%s/tmp", appDir)
	if err := os.MkdirAll(objectDir, 0o755); err != nil {
		mclog.Errorf("failed to create tmp directory: %v", err)
		http.Error(w, "failed to create tmp directory", http.StatusInternalServerError)
		return
	}

	f, err := os.Create(objectPath)
	if err != nil {
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
	writeJSON(w, map[string]string{"url": objectURL})
}

func downloadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	key := vars["key"]

	objectPath := fmt.Sprintf("%s/%s/%s", objectStorageDir, app, key)

	mclog.DebugTopicf(mclog.TopicAppObjects, "fetching object from %s", objectPath)

	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		mclog.Warnf("object not found: %s", objectPath)
		http.Error(w, "object not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, objectPath)
}
