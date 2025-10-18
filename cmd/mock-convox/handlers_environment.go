package main

import (
	"fmt"
	"net/http"
)

func getEnvironment(w http.ResponseWriter, r *http.Request) {
	env := defaultEnvMap()
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, env)
}

func setEnvironment(w http.ResponseWriter, r *http.Request) {
	var env map[string]string
	if err := decodeRequest(r.Body, &env); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if env == nil {
		env = map[string]string{}
	}
	merged := defaultEnvMap()
	for k, v := range env {
		merged[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, merged)
}
