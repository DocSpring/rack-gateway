package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

func getApps(w http.ResponseWriter, _ *http.Request) {
	apps := []App{
		{
			Name:       "rack-gateway",
			Status:     "running",
			Generation: "3",
			Release:    currentReleaseByApp["rack-gateway"],
			CreatedAt:  time.Now().Add(-720 * time.Hour),
			UpdatedAt:  time.Now().Add(-24 * time.Hour),
		},
		{
			Name:       "api",
			Status:     "running",
			Generation: "3",
			Release:    "RAPI123456",
			CreatedAt:  time.Now().Add(-720 * time.Hour),
			UpdatedAt:  time.Now().Add(-24 * time.Hour),
		},
		{
			Name:       "web",
			Status:     "running",
			Generation: "3",
			Release:    "RWEB789012",
			CreatedAt:  time.Now().Add(-480 * time.Hour),
			UpdatedAt:  time.Now().Add(-2 * time.Hour),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, apps)
}

func getApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rel := currentReleaseByApp[vars["app"]]
	if rel == "" {
		rel = "RAPP123456"
	}
	app := App{
		Name:       vars["app"],
		Status:     "running",
		Generation: "3",
		Release:    rel,
		CreatedAt:  time.Now().Add(-720 * time.Hour),
		UpdatedAt:  time.Now().Add(-24 * time.Hour),
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, app)
}

func createApp(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := decodeRequest(r.Body, &req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	app := App{
		Name:       req["name"],
		Status:     "creating",
		Generation: "3",
		Release:    "",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, app)
}

func deleteApp(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func listServices(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	mclog.DebugTopicf(mclog.TopicAppProcesses, "services list app=%s", app)
	services := []map[string]interface{}{
		{
			"name":    "web",
			"process": "web",
			"status":  "running",
		},
		{
			"name":    "worker",
			"process": "worker",
			"status":  "running",
		},
	}
	writeJSON(w, services)
}

func restartService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	service := vars["service"]
	mclog.DebugTopicf(mclog.TopicAppProcesses, "service restart app=%s service=%s", app, service)
	writeJSON(w, map[string]string{"status": "restarting"})
}

func restartApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	mclog.DebugTopicf(mclog.TopicAppProcesses, "app restart app=%s", app)
	writeJSON(w, map[string]string{"status": "restarting"})
}

func listRacks(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("[]")); err != nil {
		mclog.Errorf("failed to write racks response: %v", err)
	}
}
