package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
)

func handleReleases(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	if r.Method == http.MethodPost {
		baseID := fmt.Sprintf("RAPI%06d", time.Now().UnixNano()%1000000)
		id := nextID(baseID)
		rel := Release{
			ID:          id,
			App:         app,
			Build:       nextID("BNEW123456"),
			Description: "Created by mock env set",
			Version:     42,
			Created:     time.Now(),
			Env:         envString(),
		}
		releasesByApp[app] = append([]Release{rel}, releasesByApp[app]...)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, rel)
		return
	}
	list := releasesByApp[app]
	if list == nil {
		list = []Release{}
	}
	if lim := r.URL.Query().Get("limit"); lim == "1" && len(list) > 0 {
		list = []Release{list[0]}
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, list)
}

func getRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	var rel Release
	found := false
	for _, rr := range releasesByApp[app] {
		if rr.ID == id {
			rel = rr
			found = true
			break
		}
	}
	if !found {
		rel = Release{
			ID:          id,
			App:         app,
			Build:       "B" + id[1:],
			Description: "Deployed by mock",
			Version:     10,
			Created:     time.Now().Add(-24 * time.Hour),
		}
	}
	if rel.Env == "" {
		rel.Env = envString()
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, rel)
}

func promoteRelease(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	currentReleaseByApp[app] = id
	// Like a real rack (stdapi's RenderOK): a plain "ok" body, not JSON. The gateway streams non-JSON
	// responses, so E2E covers the path production promotes take.
	if _, err := w.Write([]byte("ok\n")); err != nil {
		mclog.Errorf("failed to write promote response: %v", err)
	}
}
