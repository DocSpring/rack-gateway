package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

func getBuilds(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	startRecent := time.Now().Add(-30 * time.Minute)
	endRecent := startRecent.Add(150 * time.Second)
	builds := []Build{
		{
			ID:          "BAPI123456",
			App:         vars["app"],
			Description: "git sha: abc123",
			Status:      "complete",
			Release:     "RAPI123456",
			Started:     startRecent,
			Ended:       endRecent,
		},
		{
			ID:          "BAPI123455",
			App:         vars["app"],
			Description: "git sha: def456",
			Status:      "complete",
			Release:     "RAPI123455",
			Started:     time.Now().Add(-49 * time.Hour),
			Ended:       time.Now().Add(-48 * time.Hour),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, builds)
}

func getBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	startRecent := time.Now().Add(-30 * time.Minute)
	endRecent := startRecent.Add(150 * time.Second)
	build := Build{
		ID:          vars["id"],
		App:         vars["app"],
		Description: "git sha: abc123",
		Status:      "complete",
		Release:     "R" + vars["id"][1:],
		Started:     startRecent,
		Ended:       endRecent,
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, build)
}

func createBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	baseID := fmt.Sprintf("BNEW%06d", time.Now().UnixNano()%1000000)
	id := nextID(baseID)
	build := Build{
		ID:          id,
		App:         app,
		Description: "created by mock",
		Status:      "running",
		// Release field is empty initially, matching real Convox API behavior.
		// The release gets created and populated when the build completes.
		Release: "",
		Started: time.Now(),
	}
	builds := []Build{build}
	if existing := releasesByApp[app]; existing != nil {
		_ = existing
	}
	_ = builds

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, build)
}
