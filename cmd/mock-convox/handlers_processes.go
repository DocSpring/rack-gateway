package main

import (
	"io"
	"net/http"
	"strings"
	"time"

	mclog "github.com/DocSpring/rack-gateway/cmd/mock-convox/logging"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func getProcesses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	processes := []Process{
		{
			Id:       "p-web-1",
			App:      vars["app"],
			Command:  "bundle exec rails server",
			Cpu:      25.5,
			Host:     "10.0.1.10",
			Image:    "registry.example.com/app:latest",
			Instance: "i-1234567890abcdef0",
			Memory:   512.0,
			Name:     "web",
			Ports:    []string{"80:3000"},
			Release:  "RAPI123456",
			Started:  time.Now().Add(-3 * time.Hour),
			Status:   "running",
		},
		{
			Id:       "p-worker-1",
			App:      vars["app"],
			Command:  "bundle exec sidekiq",
			Cpu:      15.0,
			Host:     "10.0.1.11",
			Image:    "registry.example.com/app:latest",
			Instance: "i-0987654321fedcba0",
			Memory:   256.0,
			Name:     "worker",
			Ports:    []string{},
			Release:  "RAPI123456",
			Started:  time.Now().Add(-2 * time.Hour),
			Status:   "running",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, processes)
}

func getProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	process := Process{
		Id:       vars["id"],
		App:      vars["app"],
		Command:  "bundle exec rails server",
		Cpu:      25.5,
		Host:     "10.0.1.10",
		Image:    "registry.example.com/app:latest",
		Instance: "i-1234567890abcdef0",
		Memory:   512.0,
		Name:     "web",
		Ports:    []string{"80:3000"},
		Release:  "RAPI123456",
		Started:  time.Now().Add(-3 * time.Hour),
		Status:   "running",
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, process)
}

func deleteProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	mclog.DebugTopicf(mclog.TopicAppProcesses, "delete process app=%s id=%s", vars["app"], vars["id"])
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"id":     vars["id"],
		"status": "stopping",
	})
}

func serviceProcesses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	service := vars["service"]
	rawBody := ""
	if r.Body != nil {
		if b, err := io.ReadAll(r.Body); err == nil {
			rawBody = string(b)
		}
	}
	_ = r.Body.Close()
	mclog.DebugTopicf(mclog.TopicAppProcesses, "start app=%s service=%s query=%q body=%q", app, service, r.URL.RawQuery, rawBody)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]interface{}{
		"status":  "started",
		"method":  r.Method,
		"app":     app,
		"service": service,
		"id":      nextID("proc-123456"),
	})
}

func execProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	app := vars["app"]
	id := vars["id"]
	mclog.DebugTopicf(mclog.TopicAppProcesses, "exec request app=%s pid=%s query=%q", app, id, r.URL.RawQuery)

	upgrader := websocket.Upgrader{
		CheckOrigin:  func(r *http.Request) bool { return true },
		Subprotocols: parseSubprotocols(r.Header.Get("Sec-WebSocket-Protocol")),
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		mclog.Errorf("exec upgrade error: %v", err)
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	_ = conn.WriteMessage(websocket.TextMessage, []byte("Connected to mock exec for app="+app+" pid="+id+"\n"))
	cmd := r.Header.Get("command")
	if cmd == "" {
		cmd = r.URL.Query().Get("command")
	}
	if cmd != "" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("$ "+cmd+"\n"))
		if strings.HasPrefix(cmd, "echo ") {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(strings.TrimPrefix(cmd, "echo ")+"\n"))
		} else {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("(mock output)\n"))
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("Exit code: 0\n"))
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte("Session closed.\n"))
}
