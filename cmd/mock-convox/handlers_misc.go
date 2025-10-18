package main

import (
	"net/http"
	"time"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
	"github.com/gorilla/mux"
)

func handleAPI(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	response := map[string]interface{}{
		"path":      vars["path"],
		"method":    r.Method,
		"mock":      true,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	mclog.DebugTopicf(mclog.TopicHTTP, "%s %s", r.Method, r.URL.Path)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, response)
}
